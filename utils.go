package main

import (
	"strconv"
	"strings"
)

// Returns true if a key/value pair represents a metricVal. Inputs to this method
// are always expected to be of the form "key=value"
func isMetric(key string, metricsToIncludeFromMessage map[string]bool) bool {
	return metricsToIncludeFromMessage[key] || isGauge(key) || isCumulative(key) || isCounter(key) || isSample(key)
}

// Returns true if a key represents a dimension key/value pair needs to be synced
func isDimension(key string, dimsToIncludeFromMessage map[string]bool) bool {
	return dimsToIncludeFromMessage[key] || strings.HasPrefix(key, "sfxdimension#")
}

func isCounter(key string) bool {
	return strings.HasPrefix(key, "counter#")
}

func isGauge(key string) bool {
	return strings.HasPrefix(key, "gauge#")
}

func isCumulative(key string) bool {
	return strings.HasPrefix(key, "cumulative#")
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

func mergeBoolMaps(maps ...map[string]bool) map[string]bool {
	ret := map[string]bool{}

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

func evaluateBoolEnvVariable(envVal string, defaultVal bool) (bool, error) {
	if envVal == "" {
		return defaultVal, nil
	}

	return strconv.ParseBool(envVal)
}

func evaluateIntEnvVariable(envVal string, defaultVal int64) (int64, error) {
	if envVal == "" {
		return defaultVal, nil
	}

	return strconv.ParseInt(envVal, 10, 64)
}

func makeStringSet(vals ...string) map[string]bool {
	out := make(map[string]bool)
	for _, v := range vals {
		out[v] = true
	}

	return out
}
