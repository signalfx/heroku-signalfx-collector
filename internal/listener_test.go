package internal

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
)

//nolint:funlen
func TestListenerStartWithFilter(t *testing.T) {
	dpChan := make(chan []*datapoint.Datapoint, 1)

	listener, err := NewListener(1, dpChan)
	if err != nil {
		t.Logf("Failed to setup listener")
	}

	if err := listener.Start(); err != nil {
		t.Logf("Failed to start listener")
	}

	// Test for metric name filter
	listener.metricsToExclude = map[string]bool{
		"heroku.memory_total": true,
	}

	logLine := "277 <45>1 2019-12-11T22:29:21.372436+00:00 host heroku web.1 - source=web.1 dyno=heroku.155370883.259625dd-a9c7-4987-9c86-08de28dd4f72 sample#memory_total=99.74MB sample#memory_rss=97.91MB"

	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte(logLine)))
	req.URL.RawQuery = "app_name=test"

	listener.ProcessLogs(nil, req)

	checkDatapoints(dpChan, listener.metricsToExclude, listener.dimensionPairsToExclude, t)

	// Test for dimension pair filter
	listener.metricsToExclude = map[string]bool{}

	listener.dimensionPairsToExclude = map[string]string{
		"source": "web.2",
	}

	logLine = "277 <45>1 2019-12-11T22:29:21.372436+00:00 host heroku web.2 - source=web.2 dyno=heroku.155370883.259625dd-a9c7-4987-9c86-08de28dd4f72 sample#memory_total=99.74MB sample#memory_rss=97.91MB"

	req, _ = http.NewRequest("POST", "/", bytes.NewBuffer([]byte(logLine)))
	req.URL.RawQuery = "app_name=test"

	listener.ProcessLogs(nil, req)

	checkDatapoints(dpChan, listener.metricsToExclude, listener.dimensionPairsToExclude, t)
}

func checkDatapoints(dpChan <-chan []*datapoint.Datapoint, metricFilter map[string]bool,
	dimensionFilter map[string]string, t *testing.T) {
	timeOut := time.After(1200 * time.Millisecond)

	select {
	case dps := <-dpChan:
		for _, dp := range dps {
			if !shouldExist(dp, metricFilter, dimensionFilter) {
				t.Errorf("Found datapoint that was expected to be filtered.\n%s", dp)
			}
		}
	case <-timeOut:
	}
}

func shouldExist(dp *datapoint.Datapoint, metricFilter map[string]bool,
	dimensionFilter map[string]string) bool {
	if metricFilter[dp.Metric] {
		return false
	}

	for dimKey, dimVal := range dp.Dimensions {
		if dimensionFilter[dimKey] == dimVal {
			return false
		}
	}

	return true
}
