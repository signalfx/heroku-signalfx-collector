package main

import (
	"sync"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
)

// A cumulativeCollector tracks an ever-increasing cumulative counter
type cumulativeCollector struct {
	MetricName string
	Dimensions map[string]string

	count float64
	mu    sync.Mutex
}

var _ sfxclient.Collector = &cumulativeCollector{}

// Add an item to the bucket, later reporting the result in the next report cycle.
func (c *cumulativeCollector) Add(val float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.count += val
}

// Datapoints returns the counter datapoint, or nil if there is no set metric name
func (c *cumulativeCollector) Datapoints() []*datapoint.Datapoint {
	if c.MetricName == "" {
		return []*datapoint.Datapoint{}
	}
	return []*datapoint.Datapoint{
		sfxclient.CumulativeF(c.MetricName, c.Dimensions, c.count),
	}
}
