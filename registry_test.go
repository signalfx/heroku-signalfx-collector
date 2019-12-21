package main

import (
	"testing"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
)

func setTime(r *metricRegistry, t time.Time) {
	r.currentTime = func() time.Time { return t }
}

func advanceTime(r *metricRegistry, minutes int64) {
	setTime(r, time.Unix(r.currentTime().Unix()+minutes*60, 0))
}

func TestExpiration(t *testing.T) {
	mr := newRegistry(5 * time.Minute)

	setTime(mr, time.Unix(100, 0))

	mr.registerOrGetCumulative("test1", map[string]string{"a": "1"})
	mr.registerOrGetCumulative("test1", map[string]string{"a": "2"})
	mr.registerOrGetGauge("test2", map[string]string{"a": "1"}, datapoint.Gauge)
	mr.registerOrGetGauge("test2", map[string]string{"a": "2"}, datapoint.Gauge)

	mr.Datapoints()
	if len(mr.Datapoints()) != 4 {
		t.Fatalf("Expected four datapoints")
	}

	advanceTime(mr, 4)

	mr.Datapoints()
	if len(mr.Datapoints()) != 4 {
		t.Fatalf("Expected four datapoints")
	}

	mr.registerOrGetCumulative("test1", map[string]string{"a": "3"})
	mr.registerOrGetGauge("test2", map[string]string{"a": "3"}, datapoint.Gauge)

	mr.Datapoints()
	if len(mr.Datapoints()) != 6 {
		t.Fatalf("Expected five datapoints")
	}

	advanceTime(mr, 2)

	mr.Datapoints()
	dps := mr.Datapoints()
	if len(dps) != 2 {
		t.Fatalf("Expected first four counters to be expired")
	}

	for _, dp := range dps {
		if !(dp.Metric == "test1" || dp.Metric == "test2" || dp.Dimensions["a"] == "3") {
			t.Fatalf("Wrong counter was expired")
		}
	}
}
