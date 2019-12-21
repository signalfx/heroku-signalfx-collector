package main

import (
	"testing"
	"time"
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

	mr.registerOrGetCumulative("test", map[string]string{"a": "1"})
	mr.registerOrGetCumulative("test", map[string]string{"a": "2"})
	mr.registerOrGetCumulative("test", map[string]string{"a": "3"})
	mr.registerOrGetCumulative("test", map[string]string{"a": "4"})

	mr.Datapoints()
	if len(mr.Datapoints()) != 4 {
		t.Fatalf("Expected four datapoints")
	}

	advanceTime(mr, 4)

	mr.Datapoints()
	if len(mr.Datapoints()) != 4 {
		t.Fatalf("Expected four datapoints")
	}

	mr.registerOrGetCumulative("test", map[string]string{"a": "4"})
	mr.registerOrGetCumulative("test", map[string]string{"a": "5"})

	mr.Datapoints()
	if len(mr.Datapoints()) != 5 {
		t.Fatalf("Expected five datapoints")
	}

	advanceTime(mr, 2)

	mr.Datapoints()
	dps := mr.Datapoints()
	if len(dps) != 2 {
		t.Fatalf("Expected first three counters to be expired")
	}

	for _, dp := range dps {
		if dp.Dimensions["a"] == "1" || dp.Dimensions["a"] == "2" || dp.Dimensions["a"] == "3" {
			t.Fatalf("Wrong counter was expired")
		}
	}
}
