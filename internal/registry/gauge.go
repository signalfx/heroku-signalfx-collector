package registry

import (
	"sync"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
)

// GaugeCollector tracks a gauge metric
type GaugeCollector struct {
	sync.Mutex
	MetricName string
	Dimensions map[string]string
	Type       datapoint.MetricType

	latest float64
}

var _ sfxclient.Collector = &GaugeCollector{}

// Update gauge with latest, later reporting the result in the next report cycle.
func (g *GaugeCollector) Set(val float64) {
	g.Lock()
	defer g.Unlock()

	g.latest = val
}

// Datapoints returns the latest datapoint, or nil if there is no set metric name
func (g *GaugeCollector) Datapoints() []*datapoint.Datapoint {
	g.Lock()
	defer g.Unlock()

	return []*datapoint.Datapoint{
		sfxclient.GaugeF(g.MetricName, g.Dimensions, g.latest),
	}
}
