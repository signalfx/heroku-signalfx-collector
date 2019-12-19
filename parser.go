package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"github.com/mitchellh/mapstructure"
	"github.com/signalfx/golib/v3/datapoint"
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
	ProcId    string `json:"procid"`
	Message   string `json:"message"`
}

// To parse out metricVal values when units exist in them
var numberFormat = regexp.MustCompile(`^\d+(\.\d+)?`)

// To match dyno numbers from dyno names. Dyno names the following format
// "web.45", "run.9123", "worker.2" where the prefix denotes the type of process
// the dyno is initialized with. Ror more information, see:
// https://devcenter.heroku.com/articles/process-model#process-types-vs-dynos
var dynoNumberFormat = regexp.MustCompile(`\.\d+$`)

var herokuObjectIdFormat = regexp.MustCompile(`^[0-9a-fA-F]{8}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{12}$`)

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

func (listnr *listener) processLogs(w http.ResponseWriter, req *http.Request) {
	log.Infoln(req.URL.Query().Encode())
	appName, err := getAppNameFromParams(req.URL.Query())

	if err != nil {
		log.WithFields(log.Fields{
			"error":  err,
			"params": req.URL.Query().Encode(),
		}).Error("Unable to get App name from request param (appname)")
		return
	}

	scanner := bufio.NewScanner(req.Body)
	for scanner.Scan() {
		line := scanner.Text()

		processedLog, err := detectAndParseLog(line)

		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"line":  line,
			}).Error("Error processing supported log line")
			continue
		}

		if processedLog != nil {
			listnr.registry.updateMetrics(processMetrics(processedLog, appName))
		}
	}
}

func getAppNameFromParams(values url.Values) (string, error) {
	appName := values["appname"]

	if len(appName) != 1 || appName[0] == "" {
		return "", fmt.Errorf(fmt.Sprintf("appname parameter takes exactly one value. current value: %v", appName))
	}
	return appName[0], nil
}

// Returns a logLine struct if the line matches a supported format
func detectAndParseLog(line string) (*logLine, error) {
	match := rfc5424LogFormat.FindStringSubmatch(line)

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
func processMetrics(ll *logLine, appName string) ([]*metricVal, map[string]string) {
	processType := dynoNumberFormat.ReplaceAllString(ll.ProcId, "")

	metrics, dims := ll.evaluateKeyValuePairs()
	dims = mergeStringMaps(map[string]string{"app_name": appName,}, dims)

	switch processType {
	case "router":
		metrics, dims = fixUpRouterMetricDims(metrics, dims)
	default:
		metrics, dims = fixUpRouterDynoMetricDims(metrics, dims, processType)
	}

	return metrics, dims
}

// Cleanup router metric names
func fixUpRouterMetricDims(metrics []*metricVal,
	dims map[string]string) ([]*metricVal, map[string]string) {
	for i := range metrics {
		if refinedRouterMetricNames[metrics[i].Name] != "" {
			metrics[i].Name = refinedRouterMetricNames[metrics[i].Name]
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
func fixUpRouterDynoMetricDims(metrics []*metricVal, dims map[string]string,
	processType string) ([]*metricVal, map[string]string) {
	dims["process_type"] = processType
	if dims["dyno"] != "" {
		// expects values of this form: "heroku.155370883.259625dd-a9c7-4987-9c86-08de28dd4f72"

		parsedValue := strings.Split(dims["dyno"], ".")
		if len(parsedValue) == 3 {
			match := herokuObjectIdFormat.FindAllString(parsedValue[2], 1)
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
func (ll *logLine) evaluateKeyValuePairs() ([]*metricVal, map[string]string) {
	metrics := make([]*metricVal, 0)
	dims := map[string]string{"source": ll.ProcId}

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

			metrics = append(metrics, []*metricVal{metric}...)
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
func evaluateMetric(splitPair []string) (*metricVal, error) {
	val, err := strconv.ParseFloat(splitPair[1], 64)

	// Means the value is not numeric which is indicative of units being
	// present in the metricVal value
	if err != nil {
		// If the units pertain to memory, get size in bytes
		val, err := units.RAMInBytes(splitPair[1])

		if err != nil {
			matches := numberFormat.FindAllString(splitPair[1], 1)
			if len(matches) != 1 {
				return nil, fmt.Errorf("unsupported metricVal like field encountered: [" + splitPair[0] + ", " + splitPair[1] + "]")
			}

			val, err := strconv.ParseFloat(matches[0], 64)

			if err != nil {
				return nil, fmt.Errorf("unsupported metricVal like field encountered: [" + splitPair[0] + ", " + splitPair[1] + "]")
			}
			return getMetric(splitPair[0], val)
		}
		return getMetric(splitPair[0], float64(val))
	}
	return getMetric(splitPair[0], val)
}

// Returns metricVal name, removing the datapoint type identifiers
func getMetric(rawMetricName string, metricValue float64) (*metricVal, error) {
	metricName, metricType, err := processRawMetricKey(rawMetricName)

	if err != nil {
		return nil, err
	}

	return &metricVal{
		Name:  metricName,
		Value: metricValue,
		Type:  metricType,
	}, nil
}

// Returns a processed metricVal name if it's from a supported metricVal type
func processRawMetricKey(rawMetricName string) (string, datapoint.MetricType, error) {
	if isCounter(rawMetricName) {
		return strings.Replace(rawMetricName, "counter#", "", 1), datapoint.Count, nil
	}

	if isCumulative(rawMetricName) {
		return strings.Replace(rawMetricName, "cumulative#", "", 1), datapoint.Counter, nil
	}

	// Other default metrics are categorized as gauges. This includes fields in the
	// router log message stored in routerMetricKeys
	return strings.Replace(strings.Replace(rawMetricName, "sample#", "", 1), "gauge#", "", 1), datapoint.Gauge, nil
}
