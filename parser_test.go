package main

import (
	"net/url"
	"reflect"
	"testing"
)

func TestProcessLogs(t *testing.T) {
	validInputs := []string{
		// router error log
		"277 <158>1 2012-10-11T03:47:20+00:00 host heroku router - at=error code=H12 desc=\"Request timeout\" method=GET path=/ host=myapp.herokuapp.com request_id=8601b555-6a83-4c12-8269-97c8e32cdb22 fwd=\"204.204.204.204\" dyno=web.1 connect= service=30000ms status=503 bytes=0 protocol=http",
		// router info log
		"271 <158>1 2019-12-11T16:17:53.786555+00:00 host heroku router - at=info method=GET path=\"/test\" host=aqueous-oasis-14017.herokuapp.com request_id=93bf8b6c-34b1-4eb8-9b5b-f0e72e5ce377 fwd=\"76.195.93.225\" dyno=web.1 connect=0ms service=1ms status=404 bytes=146 protocol=https",
		// dyno metricVal logs
		"277 <45>1 2019-12-11T22:29:21.372436+00:00 host heroku web.1 - source=web.1 dyno=heroku.155370883.259625dd-a9c7-4987-9c86-08de28dd4f72 sample#memory_total=99.74MB sample#memory_rss=97.91MB sample#memory_cache=1.83MB sample#memory_swap=0.00MB sample#memory_pgpgin=355603pages sample#memory_pgpgout=333646pages sample#memory_quota=512.00MB",
		"277 <45>1 2019-12-11T22:29:21.372436+00:00 host heroku web.2 - source=web.2 dyno=heroku.155370883.e764d0ed-b239-4048-9caa-38a78dfeb6d0 sample#load_avg_1m=0.00",
	}

	expectedParsedLog := []*logLine{
		{
			PRI:       "158",
			Version:   "1",
			Timestamp: "2012-10-11T03:47:20+00:00",
			Hostname:  "host",
			Appname:   "heroku",
			ProcId:    "router",
			Message:   "at=error code=H12 desc=\"Request timeout\" method=GET path=/ host=myapp.herokuapp.com request_id=8601b555-6a83-4c12-8269-97c8e32cdb22 fwd=\"204.204.204.204\" dyno=web.1 connect= service=30000ms status=503 bytes=0 protocol=http",
		}, {
			PRI:       "158",
			Version:   "1",
			Timestamp: "2019-12-11T16:17:53.786555+00:00",
			Hostname:  "host",
			Appname:   "heroku",
			ProcId:    "router",
			Message:   "at=info method=GET path=\"/test\" host=aqueous-oasis-14017.herokuapp.com request_id=93bf8b6c-34b1-4eb8-9b5b-f0e72e5ce377 fwd=\"76.195.93.225\" dyno=web.1 connect=0ms service=1ms status=404 bytes=146 protocol=https",
		}, {
			PRI:       "45",
			Version:   "1",
			Timestamp: "2019-12-11T22:29:21.372436+00:00",
			Hostname:  "host",
			Appname:   "heroku",
			ProcId:    "web.1",
			Message:   "source=web.1 dyno=heroku.155370883.259625dd-a9c7-4987-9c86-08de28dd4f72 sample#memory_total=99.74MB sample#memory_rss=97.91MB sample#memory_cache=1.83MB sample#memory_swap=0.00MB sample#memory_pgpgin=355603pages sample#memory_pgpgout=333646pages sample#memory_quota=512.00MB",
		}, {
			PRI:       "45",
			Version:   "1",
			Timestamp: "2019-12-11T22:29:21.372436+00:00",
			Hostname:  "host",
			Appname:   "heroku",
			ProcId:    "web.2",
			Message:   "source=web.2 dyno=heroku.155370883.e764d0ed-b239-4048-9caa-38a78dfeb6d0 sample#load_avg_1m=0.00",
		},
	}

	numExpectedMetrics := []int{2, 3, 7, 1}
	numExpectedDimensions := []int{8, 7, 5, 5}

	for i, input := range validInputs {
		actual, _ := detectAndParseLog(input)

		if *expectedParsedLog[i] != *actual {
			t.Logf("Expected: %s", *expectedParsedLog[i])
			t.Logf("Actual: %s", *actual)
			t.Error("Parsed log output does not match expected")
		}

		metrics, dims := processMetrics(actual, map[string]string{
			"app_name": "test-app",
		})

		if numExpectedMetrics[i] != len(metrics) {
			t.Logf("Actual: %v", metrics)
			t.Errorf("Expected %d metrics, received %d metrics", numExpectedMetrics[i], len(metrics))
		}

		if numExpectedDimensions[i] != len(dims) {
			t.Logf("Actual: %s", dims)
			t.Errorf("Expected %d dimensions, received %d dimensions", numExpectedDimensions[i], len(dims))
		}
	}

}

func TestGetDimensionParisFromParams(t *testing.T) {
	values := url.Values{
		"dim1": []string{"val1", "val2"},
		"dim2": []string{"val1"},
	}

	_, err := getDimensionParisFromParams(values)

	// Expected to error out since "app_name" is not passed in
	if err == nil {
		t.Errorf("Expected to fail since app_name is not passed in")
	}

	values["app_name"] = []string{"test"}
	expected := map[string]string{
		"dim1":     "val1",
		"dim2":     "val1",
		"app_name": "test",
	}

	actual, _ := getDimensionParisFromParams(values)

	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("Expected %v datapoints, received %v datapoints", expected, actual)
	}
}
