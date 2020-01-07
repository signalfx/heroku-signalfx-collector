package registry

import (
	"testing"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/stretchr/testify/require"
)

func setTime(r *MetricRegistry, t time.Time) {
	r.currentTime = func() time.Time { return t }
}

func advanceTime(r *MetricRegistry, minutes int64) {
	setTime(r, time.Unix(r.currentTime().Unix()+minutes*60, 0))
}

func TestExpiration(t *testing.T) {
	mr := New(5 * time.Minute)

	setTime(mr, time.Unix(100, 0))

	mr.UpdateMetric(&MetricVal{Name: "test1", Type: datapoint.Counter, Value: 1.0}, map[string]string{"a": "1"})
	mr.UpdateMetric(&MetricVal{Name: "test1", Type: datapoint.Counter, Value: 2.0}, map[string]string{"a": "2"})
	mr.UpdateMetric(&MetricVal{Name: "test2", Type: datapoint.Gauge, Value: 1.0}, map[string]string{"a": "1"})
	mr.UpdateMetric(&MetricVal{Name: "test2", Type: datapoint.Gauge, Value: 1.5}, map[string]string{"a": "2"})
	mr.UpdateMetric(&MetricVal{Name: "test2", Type: datapoint.Count, Value: 3}, map[string]string{"a": "1"})

	mr.Datapoints()
	require.Equal(t, 5, len(mr.Datapoints()))

	advanceTime(mr, 4)

	require.Equal(t, 5, len(mr.Datapoints()))

	mr.UpdateMetric(&MetricVal{Name: "test1", Type: datapoint.Counter, Value: 2.0}, map[string]string{"a": "3"})
	mr.UpdateMetric(&MetricVal{Name: "test2", Type: datapoint.Gauge, Value: 3.0}, map[string]string{"a": "3"})

	require.Equal(t, 7, len(mr.Datapoints()))

	advanceTime(mr, 2)

	dps := mr.Datapoints()
	require.Equalf(t, 2, len(dps), "Expected first five to be expired")

	for _, dp := range dps {
		if !(dp.Metric == "test1" || dp.Metric == "test2" || dp.Dimensions["a"] == "3") {
			t.Fatalf("Wrong counter was expired")
		}
	}
}
