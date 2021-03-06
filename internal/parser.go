package internal

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/mitchellh/mapstructure"
	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/heroku-signalfx-collector/internal/registry"
	log "github.com/sirupsen/logrus"
)

// Based on docs here: https://tools.ietf.org/html/rfc5424#section-6
// and discussion here: https://stackoverflow.com/questions/25163830/explain-format-of-heroku-logs
var rfc5424LogFormat = regexp.MustCompile(`\<(?P<pri>\d+)\>(?P<version>1) (?P<timestamp>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{6})?\+\d{2}:\d{2}) (?P<hostname>[a-z0-9\-\_\.]+) (?P<appname>[a-z0-9\.-]+) (?P<procid>[a-z0-9\-\_\.]+) (?P<msgid>\-) (?P<message>.*)$`)
var regexGroups = rfc5424LogFormat.SubexpNames()

type logLine struct {
	PRI       string `json:"pri"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
	Hostname  string `json:"hostname"`
	Appname   string `json:"appname"`
	ProcID    string `json:"procid"`
	Message   string `json:"message"`
}

// Format based on docs here,
// https://devcenter.heroku.com/articles/platform-api-reference#custom-types
var herokuObjectIDFormat = regexp.MustCompile(`^[0-9a-fA-F]{8}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{12}$`)

// These fields are based on Heroku docs. For more information, see here:
// https://devcenter.heroku.com/articles/http-routing#heroku-router-log-format
var routerDimensionKeys = makeStringSet("status", "method", "dyno", "protocol", "host", "code")
var dynoDimensionKeys = makeStringSet("dyno")
var herokuDimensionKeys = mergeBoolMaps(dynoDimensionKeys, routerDimensionKeys)

// These fields are based on Heroku docs. For more information, see here:
// https://devcenter.heroku.com/articles/http-routing#heroku-router-log-format
// Also note that all the 3 strings stated here are all from router logs
var herokuMetricKeys = makeStringSet("connect", "service", "bytes")

// In some cases metricVal names derived from the logs don't make a lot of sense.
// Have an alternative name for such metrics
var refinedRouterMetricNames = map[string]string{
	"connect": "heroku.router_request_connect_time_millis",
	"service": "heroku.router_request_service_time_millis",
	"bytes":   "heroku.router_response_bytes",
}

func getDimensionPairsFromParams(values url.Values) (map[string]string, error) {
	dims := map[string]string{}

	for dimKey, dimValue := range values {
		// if parameters has multiple value, dimensionalize only the first one
		if len(dimValue) > 0 && dimValue[0] != "" {
			dims[dimKey] = dimValue[0]
		}
	}

	if dims["app_name"] == "" {
		return nil, fmt.Errorf("app_name parameter takes exactly one value. current value: %v", values["app_name"])
	}

	return dims, nil
}

// Returns a logLine struct if the line matches a supported format
func detectAndParseLog(line string) (*logLine, error) {
	match := rfc5424LogFormat.FindStringSubmatch(line)

	// Simply ignore logs that not match the specified format
	if len(match) != len(regexGroups) {
		return nil, nil
	}

	mapped := make(map[string]string)

	for i, name := range regexGroups {
		if i != 0 && name != "" {
			mapped[name] = match[i]
		}
	}

	result := logLine{}
	err := mapstructure.Decode(mapped, &result)

	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Returns datapoints processed from a logLine struct. Specifically, this
// method processes the message field in it. This method assumes that the
// message has information about dimensions and metrics, always in the
// following form and this is the only part of the message that's processed
// "key1=value1 key2=value2 key3=value3 sample#metric_name=metric_value"
func processMetrics(ll *logLine, dimsFromParmas map[string]string) ([]*registry.MetricVal, map[string]string) {
	// To match dyno numbers from dyno names. Dyno names the following format
	// "web.45", "run.9123", "worker.2" where the prefix denotes the type of process
	// the dyno is initialized with. Ror more information, see:
	// https://devcenter.heroku.com/articles/process-model#process-types-vs-dynos
	processType := strings.Split(ll.ProcID, ".")[0]

	metrics, dims := ll.evaluateKeyValuePairs()

	// dimensions from parameters will take precedence over dimensions from logs
	// in case there are duplicate keys
	dims = mergeStringMaps(dimsFromParmas, dims)

	switch processType {
	case "router":
		metrics, dims = fixUpRouterMetrics(metrics, dims)
	default:
		metrics, dims = fixUpDynoMetrics(metrics, dims, processType)
	}

	return metrics, dims
}

// Cleanup router metric names
func fixUpRouterMetrics(metrics []*registry.MetricVal, dims map[string]string) ([]*registry.MetricVal, map[string]string) {
	for i := range metrics {
		if refinedRouterMetricNames[metrics[i].Name] != "" {
			metrics[i].Name = refinedRouterMetricNames[metrics[i].Name]
			metrics[i].Type = datapoint.Counter
		}
	}

	return metrics, dims
}

// Handle post processing of metrics and dims collected. More specifically,
// (1) add "process_type" dimension which has the value set to the process
// with which the dyno is initialized. (2) derive "dyno_id" dimension from
// existing "dyno" field collected. (3) add "dyno" dimension with the same
// value as source. This will make it easy to filter both router and dyno
// metrics by a single dimension
func fixUpDynoMetrics(metrics []*registry.MetricVal, dims map[string]string, processType string) ([]*registry.MetricVal, map[string]string) {
	dims["process_type"] = processType
	if dims["dyno"] != "" {
		// expects values of this form: "heroku.155370883.259625dd-a9c7-4987-9c86-08de28dd4f72"
		parsedValue := strings.Split(dims["dyno"], ".")
		if len(parsedValue) == 3 {
			match := herokuObjectIDFormat.FindAllString(parsedValue[2], 1)
			if len(match) == 1 {
				dims["dyno_id"] = match[0]
			}
		}

		// Remove once "dyno_id" is derived
		delete(dims, "dyno")
	}

	// add dyno name as a dimension
	if dims["source"] != "" {
		dims["dyno"] = dims["source"]
	}

	return metrics, dims
}

// Gets metrics and dimensions from the message field on a log line. Note that this
// method adds "source" dimensions by default on all  metrics
func (ll *logLine) evaluateKeyValuePairs() ([]*registry.MetricVal, map[string]string) {
	metrics := make([]*registry.MetricVal, 0)
	dims := map[string]string{"source": ll.ProcID}

	for _, pair := range strings.Split(ll.Message, " ") {
		splitPair := strings.Split(pair, "=")

		// only bother about key/value pairs separated by =
		if len(splitPair) != 2 {
			continue
		}

		log.WithFields(log.Fields{
			"key/value pair": pair,
		}).Debug("Processing key/value pair in log message")

		if isMetric(splitPair[0], herokuMetricKeys) {
			metric, err := evaluateMetric(splitPair)

			if err != nil {
				log.WithFields(log.Fields{
					"debug":          err,
					"key-value pair": pair,
				}).Debug("Error making metricVal from key/value pair in log message. Will be dropped.")

				continue
			}

			metrics = append(metrics, []*registry.MetricVal{metric}...)

			continue
		}

		// Dimensions for custom metrics
		if isDimension(splitPair[0], herokuDimensionKeys) {
			dims = mergeStringMaps(dims, map[string]string{
				strings.Replace(splitPair[0], "sfxdimension#", "", 1): splitPair[1],
			})
		}
	}

	return metrics, dims
}

// Evaluates metrics in the message of a log line. This method assumes the
// input will always be of the following form ["sample#metric_name", "metric_value"]
// where "metric_value" may or may not include units
func evaluateMetric(splitPair []string) (*registry.MetricVal, error) {
	val, err := getNumericValue(splitPair[1])

	if err != nil {
		return nil, err
	}

	return getMetric(splitPair[0], *val)
}

// Returns a value stripping out the units for supported units.
// If an unsupported unit is encountered, the metric will be dropped.
func getNumericValue(value string) (*float64, error) {
	out := new(float64)
	// If the units pertain to memory, get size in bytes
	bytes, err := units.RAMInBytes(value)

	if err == nil {
		*out = float64(bytes)
		return out, nil
	}

	duration, err := time.ParseDuration(value)

	if err == nil {
		*out = duration.Seconds() * 1000
		return out, nil
	}

	// Check for units called "pages" from memory_pgpgin and memory_pgpgout
	// which are standard metrics in Heroku
	value = strings.Replace(value, "pages", "", 1)

	numericValue, err := strconv.ParseFloat(value, 64)

	if err != nil {
		return nil, fmt.Errorf("found unsupported metric unit in %s", value)
	}

	*out = numericValue

	return out, nil
}

// Returns metricVal name, removing the datapoint type identifiers
func getMetric(rawMetricName string, metricValue float64) (*registry.MetricVal, error) {
	metricName, metricType := processRawMetricKey(rawMetricName)

	return &registry.MetricVal{
		Name:  metricName,
		Value: metricValue,
		Type:  metricType,
	}, nil
}

// Returns a processed metricVal name if it's from a supported metricVal type
func processRawMetricKey(rawMetricName string) (string, datapoint.MetricType) {
	if isCounter(rawMetricName) {
		return strings.Replace(rawMetricName, "counter#", "", 1), datapoint.Count
	}

	if isCumulative(rawMetricName) {
		return strings.Replace(rawMetricName, "cumulative#", "", 1), datapoint.Counter
	}

	// Standard Heroku metrics log run-time metrics identified as samples in the log message.
	// Remove the sample# prefix from such metric names, and also add a "heroku" prefix to
	// make metrics easily searchable
	if isSample(rawMetricName) {
		return strings.Replace(rawMetricName, "sample#", "heroku.", 1), datapoint.Gauge
	}

	// Other default metrics are categorized as gauges. This includes fields in the
	// router log message stored in routerMetricKeys.
	return strings.Replace(rawMetricName, "gauge#", "", 1), datapoint.Gauge
}
