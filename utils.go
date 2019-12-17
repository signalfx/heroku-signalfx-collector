package main

import (
	"strconv"
	"strings"
)

// Returns true if a key/value pair represents a metricVal. Inputs to this method
// are always expected to be of the form "key=value"
func isMetric(key string, metricsToIncludeFromMessage map[string]bool) bool {
	return metricsToIncludeFromMessage[key] || isGauge(key) || isCounter(key) || isCumulative(key) || isSample(key)
}

// Returns true if a key represents a dimension key/value pair needs to be synced
func isDimension(key string, dimsToIncludeFromMessage map[string]bool) bool {
	return dimsToIncludeFromMessage[key] || strings.HasPrefix(key, "sfxdimension#")
}

func isGauge(key string) bool {
	return strings.HasPrefix(key, "gauge#")
}

func isCumulative(key string) bool {
	return strings.HasPrefix(key, "cumulative#")
}

func isCounter(key string) bool {
	return strings.HasPrefix(key, "counter#")
}

func isSample(key string) bool {
	return strings.HasPrefix(key, "sample#")
}

func mergeStringMaps(maps ...map[string]string) map[string]string {
	ret := map[string]string{}

	for _, m := range maps {
		for k, v := range m {
			ret[k] = v
		}
	}

	return ret
}

func makeSetOfStringsFromArray(metricsToExlcude []string) map[string]bool {
	ret := map[string]bool{}

	for _, m := range metricsToExlcude {
		ret[m] = true
	}

	return ret
}

func evaluateBoolEnvVariable(envKey string, defaultVal bool) (bool, error) {
	if envKey == "" {
		return defaultVal, nil
	}

	return strconv.ParseBool(envKey)
}
