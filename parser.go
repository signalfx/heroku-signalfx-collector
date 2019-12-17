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
	"github.com/signalfx/golib/v3/sfxclient"
	log "github.com/sirupsen/logrus"
)

// Based on docs here: https://tools.ietf.org/html/rfc5424#section-6
// and discussion here: https://stackoverflow.com/questions/25163830/explain-format-of-heroku-logs
var rfc5424LogFormat = regexp.MustCompile(`\<(?P<pri>\d+)\>(?P<version>1) (?P<timestamp>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{6})?\+\d{2}:\d{2}) (?P<hostname>[a-z0-9\-\_\.]+) (?P<appname>[a-z0-9\.-]+) (?P<procid>[a-z0-9\-\_\.]+) (?P<msgid>\-) (?P<message>.*)$`)

type logLine struct {
	PRI       string `json:pri`
	Version   string `json:version`
	Timestamp string `json:timestamp`
	Hostname  string `json:hostname`
	Appname   string `json:appname`
	ProcId    string `json:procid`
	Message   string `json:message`
}

type metricVal struct {
	Name  string
	Type  datapoint.MetricType
	Value float64
}

// To parse out metricVal values when units exist in them
var numberFormat = regexp.MustCompile(`^\d+(\.\d+)?`)

// To match dyno numbers from dyno names. Dyno names the following format
// "web.45", "run.9123", "worker.2" where the prefix denotes the type of process
// the dyno is initialized with. Ror more information, see:
// https://devcenter.heroku.com/articles/process-model#process-types-vs-dynos
var dynoNumberFormat = regexp.MustCompile(`\.\d+$`)

var herokuObjectIdFormat = regexp.MustCompile(`^[0-9a-fA-F]{8}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{12}$`)

var dynoDimensionKeys = map[string]bool{"dyno": true}

// These fields are based on Heroku docs. For more information, see here:
// https://devcenter.heroku.com/articles/http-routing#heroku-router-log-format
var routerDimensionKeys = map[string]bool{
	"status":   true,
	"method":   true,
	"dyno":     true,
	"protocol": true,
	"host":     true,
	"code":     true,
}

var routerMetricKeys = map[string]bool{
	"connect": true,
	"service": true,
	"bytes":   true,
}

// In some cases metricVal names derived from the logs don't make a lot of sense.
// Have an alternative name for such metrics
var refinedMetricNames = map[string]string{
	"connect": "heroku.router_request_connect_time_millis",
	"service": "heroku.router_request_service_time_millis",
	"bytes":   "heroku.router_response_bytes",
}

func (sw *listener) processLogs(w http.ResponseWriter, req *http.Request) {
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
			sw.dps <- collectDatapoints(processedLog, appName)
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
	regexGroups := rfc5424LogFormat.SubexpNames()

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
func collectDatapoints(ll *logLine, appName string) []*datapoint.Datapoint {
	processType := dynoNumberFormat.ReplaceAllString(ll.ProcId, "")
	metrics, dims := make([]*metricVal, 0), map[string]string{}
	appNameDim := map[string]string{
		"appName": appName,
	}

	// Router logs are special
	if processType == "router" {
		metrics, dims = collectDatapointsForRouter(ll)
		return getSFXDatapoints(metrics, mergeStringMaps(appNameDim, dims))
	}

	metrics, dims = collectDatapointsForApp(ll, processType)
	return getSFXDatapoints(metrics, mergeStringMaps(appNameDim, dims))
}

// Collects datapoints from an app. For more information, see here:
// https://devcenter.heroku.com/articles/metrics
func collectDatapointsForApp(ll *logLine, processType string) ([]*metricVal, map[string]string) {
	metrics, dims := evaluateKeyValuePairs(
		ll, map[string]string{"process_type": processType},
		map[string]bool{}, dynoDimensionKeys,
	)
	return metrics, dims
}

// Returns datapoints from router logs. For more information about data exposed, see:
// https://devcenter.heroku.com/articles/http-routing#heroku-router-log-format
func collectDatapointsForRouter(ll *logLine) ([]*metricVal, map[string]string) {
	metrics, dims := evaluateKeyValuePairs(
		ll, map[string]string{}, routerMetricKeys,
		routerDimensionKeys,
	)
	return metrics, dims
}

// Gets metrics and dimensions from the message field on a log line. Note that this
// method adds "source" and "process_type" dimensions on all datapoints by default,
// and its values would be dyno name and process type using which the dyno was
// initialized respectively
func evaluateKeyValuePairs(ll *logLine, dims map[string]string,
	metricsToIncludeFromMessage map[string]bool,
	dimsToIncludeFromMessage map[string]bool) ([]*metricVal, map[string]string) {
	metrics := make([]*metricVal, 0)
	dims = mergeStringMaps(dims, map[string]string{"source": ll.ProcId})

	for _, pair := range strings.Split(ll.Message, " ") {
		splitPair := strings.Split(pair, "=")

		// only bother about key/value pairs separated by =
		if len(splitPair) != 2 {
			continue
		}

		log.WithFields(log.Fields{
			"key/value pair": pair,
		}).Debug("Processing key/value pair in log message")

		if isMetric(splitPair[0], metricsToIncludeFromMessage) {
			metric, err := evaluateMetric(splitPair, ll.ProcId == "router")

			if err != nil {
				log.WithFields(log.Fields{
					"debug":          err,
					"key-value pair": pair,
				}).Debug("Error making metricVal from key/value pair in log message. Will be dropped.")
				continue
			}

			metrics = append(metrics, []*metricVal{metric,}...)
			continue
		}

		// Dimensions for custom metrics
		if isDimension(splitPair[0], dimsToIncludeFromMessage) {
			dims = mergeStringMaps(dims, processDimensionPair(splitPair, ll.ProcId))
		}
	}

	return metrics, dims
}

func getSFXDatapoints(metrics []*metricVal, dims map[string]string) []*datapoint.Datapoint {
	out := make([]*datapoint.Datapoint, 0)

	for _, metric := range metrics {
		datum := &datapoint.Datapoint{}
		switch metric.Type {
		case datapoint.Gauge:
			datum = sfxclient.GaugeF(metric.Name, dims, metric.Value)
		case datapoint.Counter:
			datum = sfxclient.Counter(metric.Name, dims, int64(metric.Value))
		case datapoint.Count:
			datum = sfxclient.CumulativeF(metric.Name, dims, metric.Value)
		}

		log.Debugln(datum)

		out = append(out, []*datapoint.Datapoint{datum,}...)
	}

	return out
}

func processDimensionPair(splitPair []string, prodId string) map[string]string {
	out := make(map[string]string)

	switch splitPair[0] {
	// metricVal logs from dynos have the dyno id encoded in a field called "dyno"
	// Parse this value out and sync it as "dyno_id" dimension
	case "dyno":
		// This dimension has different values in router logs vs dyno logs
		if prodId != "router" {
			parsedValue := strings.Split(splitPair[1], ".")

			if len(parsedValue) == 3 {
				match := herokuObjectIdFormat.FindAllString(parsedValue[2], 1)
				if len(match) == 1 {
					out["dyno_id"] = match[0]
				}
			}
		} else {
			out[splitPair[0]] = splitPair[1]
		}
	default:
		out[strings.Replace(splitPair[0], "sfxdimension#", "", 1)] = splitPair[1]
	}

	return out
}

// Evaluates metrics in the message of a log line. This method assumes the
// input will always be of the following form ["sample#metric_name", "metric_value"]
// where "metric_value" may or may not include units
func evaluateMetric(splitPair []string, isRouterMetric bool) (*metricVal, error) {
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
			return getMetric(splitPair[0], float64(val), isRouterMetric)
		}
		return getMetric(splitPair[0], float64(val), isRouterMetric)
	}
	return getMetric(splitPair[0], float64(val), isRouterMetric)
}

// Returns metricVal name, removing the datapoint type identifiers
func getMetric(rawMetricName string, metricValue float64, isRouterMetric bool) (*metricVal, error) {
	metricName, metricType, err := processRawMetricKey(rawMetricName)

	if err != nil {
		return nil, err
	}

	if isRouterMetric && refinedMetricNames[metricName] != "" {
		metricName = refinedMetricNames[metricName]
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
