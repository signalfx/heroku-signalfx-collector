package registry

import (
	"sync"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
)

// A CounterCollector tracks a delta increment in between datapoint collection
// cycles.
type CounterCollector struct {
	sync.Mutex

	MetricName string
	Dimensions map[string]string

	count float64
}

var _ sfxclient.Collector = &CounterCollector{}

// Add an item to the bucket, later reporting the result in the next report cycle.
func (c *CounterCollector) Add(val float64) {
	c.Lock()
	defer c.Unlock()

	c.count += val
}

// Datapoints returns the counter datapoint, or nil if there is no set metric name
func (c *CounterCollector) Datapoints() []*datapoint.Datapoint {
	c.Lock()
	defer c.Unlock()

	val := c.count
	c.count = 0.0

	return []*datapoint.Datapoint{
		datapoint.New(c.MetricName, c.Dimensions, datapoint.NewFloatValue(val), datapoint.Count, time.Time{}),
	}
}
