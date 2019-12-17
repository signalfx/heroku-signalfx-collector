package registry

import (
	"sync"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
)

// A cumulativeCollector tracks an ever-increasing cumulative counter
type CumulativeCollector struct {
	sync.Mutex
	MetricName string
	Dimensions map[string]string

	count float64
}

var _ sfxclient.Collector = &CumulativeCollector{}

// Add an item to the bucket, later reporting the result in the next report cycle.
func (c *CumulativeCollector) Add(val float64) {
	c.Lock()
	defer c.Unlock()

	c.count += val
}

// Datapoints returns the counter datapoint, or nil if there is no set metric name
func (c *CumulativeCollector) Datapoints() []*datapoint.Datapoint {
	c.Lock()
	defer c.Unlock()

	return []*datapoint.Datapoint{
		sfxclient.CumulativeF(c.MetricName, c.Dimensions, c.count),
	}
}
