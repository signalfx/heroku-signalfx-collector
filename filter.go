package main

import (
	"strings"

	log "github.com/sirupsen/logrus"
)

func getMetricsToExclude(metricsToExlcudeEnv string) map[string]bool {
	metricsToExlcude := strings.Split(metricsToExlcudeEnv, ",")
	out := makeSetOfStringsFromArray(metricsToExlcude)

	log.WithFields(log.Fields{
		"metricVal filter": "Metrics to exclude",
	}).Info(metricsToExlcude)

	return out
}

func getDimensionPairsToExclude(dimensionPairsEnv string) map[string]string {
	dimensionPairs := strings.Split(dimensionPairsEnv, ",")
	out := make(map[string]string)
	for _, pair := range dimensionPairs {
		splitPair := strings.Split(pair, "=")
		out[splitPair[0]] = splitPair[1]
	}

	log.WithFields(log.Fields{
		"dimension filter": "Dimension key-value pairs to exclude",
	}).Info(out)

	return out
}
