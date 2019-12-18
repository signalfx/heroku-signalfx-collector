package main

import (
	"sync"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
)

// A gaugeCollector tracks a gauge metric
type gaugeCollector struct {
	MetricName string
	Dimensions map[string]string
	Type       datapoint.MetricType

	latest float64
	mu     sync.Mutex
}

var _ sfxclient.Collector = &gaugeCollector{}

// Update gauge with latest, later reporting the result in the next report cycle.
func (g *gaugeCollector) Latest(val float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.latest += val
}

// Datapoints returns the latest datapoint, or nil if there is no set metric name
func (g *gaugeCollector) Datapoints() []*datapoint.Datapoint {
	if g.MetricName == "" {
		return []*datapoint.Datapoint{}
	}

	switch g.Type {
	case datapoint.Count:
		return []*datapoint.Datapoint{
			sfxclient.Counter(g.MetricName, g.Dimensions, int64(g.latest)),
		}
	}

	return []*datapoint.Datapoint{
		sfxclient.GaugeF(g.MetricName, g.Dimensions, g.latest),
	}
}
