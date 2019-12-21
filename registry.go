package main

import (
	"sort"
	"sync"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
	log "github.com/sirupsen/logrus"
)

type metricId string

// Keeps track of all the metrics that have been reporting
type metricRegistry struct {
	sync.RWMutex

	cumulativeCounters map[metricId]*cumulativeCollector
	gauges             map[metricId]*gaugeCollector
}

func (mr *metricRegistry) Datapoints() []*datapoint.Datapoint {
	mr.RLock()
	defer mr.RUnlock()

	var out []*datapoint.Datapoint

	for id := range mr.gauges {
		out = append(out, mr.gauges[id].Datapoints()...)
	}

	for id := range mr.cumulativeCounters {
		out = append(out, mr.cumulativeCounters[id].Datapoints()...)
	}

	return out
}

type metricVal struct {
	Name  string
	Type  datapoint.MetricType
	Value float64
}

var _ sfxclient.Collector = &metricRegistry{}

func newRegistry() *metricRegistry {
	return &metricRegistry{
		cumulativeCounters: map[metricId]*cumulativeCollector{},
		gauges:             map[metricId]*gaugeCollector{},
	}
}

func (mr *metricRegistry) updateMetrics(mvs []*metricVal, dims map[string]string) {
	for _, mv := range mvs {
		mr.updateMetric(mv, dims)
	}
}

func (mr *metricRegistry) updateMetric(mv *metricVal, dims map[string]string) {
	id := idForMetric(mv.Name, dims)

	switch mv.Type {
	case datapoint.Gauge:
		g := mr.registerOrGetGauge(mv.Name, dims, id, mv.Type)
		g.Latest(mv.Value)
	case datapoint.Count:
		g := mr.registerOrGetGauge(mv.Name, dims, id, mv.Type)
		g.Latest(mv.Value)
	case datapoint.Counter:
		cu := mr.registerOrGetCumulative(mv.Name, dims, id)
		cu.Add(mv.Value)
	default:
		log.WithFields(log.Fields{
			"metric": mv.Name,
			"type":   mv.Type,
		}).Warn("Unsupported metric type")
	}

}

func (mr *metricRegistry) registerOrGetCumulative(name string, dims map[string]string, id metricId) *cumulativeCollector {
	mr.Lock()
	defer mr.Unlock()

	if c := mr.cumulativeCounters[id]; c == nil {
		mr.cumulativeCounters[id] = &cumulativeCollector{
			MetricName: name,
			Dimensions: dims,
		}
	}

	return mr.cumulativeCounters[id]
}

func (mr *metricRegistry) registerOrGetGauge(name string, dims map[string]string, id metricId, metricType datapoint.MetricType) *gaugeCollector {
	mr.Lock()
	defer mr.Unlock()

	if c := mr.gauges[id]; c == nil {
		mr.gauges[id] = &gaugeCollector{
			MetricName: name,
			Dimensions: dims,
			Type:       metricType,
		}
	}

	return mr.gauges[id]
}

func idForMetric(name string, dims map[string]string) metricId {
	id := name + "|"

	for _, key := range sortKeys(dims) {
		id += key + ":" + dims[key] + "|"
	}

	return metricId(id)
}

func sortKeys(m map[string]string) []string {
	var keys sort.StringSlice
	for key := range m {
		keys = append(keys, key)
	}

	if keys != nil {
		keys.Sort()
	}
	return []string(keys)
}
