package internal

import (
	"reflect"
	"testing"
)

func TestGetMetricsToExclude(t *testing.T) {
	metricsToExlcudeEnv := "metric1,metric2,metric3"

	expected := map[string]bool{
		"metric1": true,
		"metric2": true,
		"metric3": true,
	}

	actual := getMetricsToExclude(metricsToExlcudeEnv)

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Expected: %v, Actual: %v", expected, actual)
	}
}

func TestGetDimensionPairsToExclude(t *testing.T) {
	dimensionPairsEnv := "key1=val1,key2=val2,key3=val3"

	expected := map[string]string{
		"key1": "val1",
		"key2": "val2",
		"key3": "val3",
	}

	actual := getDimensionPairsToExclude(dimensionPairsEnv)

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Expected: %v, Actual: %v", expected, actual)
	}
}
