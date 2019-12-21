package main

import (
	"container/list"
	"sort"
	"sync"
	"time"

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

	// A linked list that we keep sorted by access time so that we can very
	// quickly tell which collectors are expired and should be deleted.
	lastAccessList list.List
	// A map optimizing lookup of the access time elements that are used in the
	// above linked list.
	lastAccesses map[metricId]*list.Element

	expiryTimeout time.Duration

	// This is the source of truth for the current time and exists to make unit
	// testing easier
	currentTime func() time.Time
}

func (mr *metricRegistry) Datapoints() []*datapoint.Datapoint {
	mr.RLock()

	var out []*datapoint.Datapoint

	for id := range mr.gauges {
		out = append(out, mr.gauges[id].Datapoints()...)
	}

	for id := range mr.cumulativeCounters {
		out = append(out, mr.cumulativeCounters[id].Datapoints()...)
	}

	mr.RUnlock()

	mr.purgeOldCollectors()

	return out
}

type metricVal struct {
	Name  string
	Type  datapoint.MetricType
	Value float64
}

var _ sfxclient.Collector = &metricRegistry{}

func newRegistry(expiryTimeout time.Duration) *metricRegistry {
	return &metricRegistry{
		cumulativeCounters: map[metricId]*cumulativeCollector{},
		gauges:             map[metricId]*gaugeCollector{},
		lastAccesses:       make(map[metricId]*list.Element),
		expiryTimeout:      expiryTimeout,
		currentTime:        time.Now,
	}
}

func (mr *metricRegistry) updateMetrics(mvs []*metricVal, dims map[string]string) {
	for _, mv := range mvs {
		mr.updateMetric(mv, dims)
	}
}

func (mr *metricRegistry) updateMetric(mv *metricVal, dims map[string]string) {
	switch mv.Type {
	case datapoint.Gauge:
		g := mr.registerOrGetGauge(mv.Name, dims, mv.Type)
		g.Latest(mv.Value)
	case datapoint.Count:
		g := mr.registerOrGetGauge(mv.Name, dims, mv.Type)
		g.Latest(mv.Value)
	case datapoint.Counter:
		cu := mr.registerOrGetCumulative(mv.Name, dims)
		cu.Add(mv.Value)
	default:
		log.WithFields(log.Fields{
			"metric": mv.Name,
			"type":   mv.Type,
		}).Warn("Unsupported metric type")
	}

}

func (mr *metricRegistry) registerOrGetCumulative(name string, dims map[string]string) *cumulativeCollector {
	mr.Lock()
	defer mr.Unlock()

	id := idForMetric(name, dims)
	if c := mr.cumulativeCounters[id]; c == nil {
		mr.cumulativeCounters[id] = &cumulativeCollector{
			MetricName: name,
			Dimensions: dims,
		}
	}

	mr.markUsed(id)
	return mr.cumulativeCounters[id]
}

func (mr *metricRegistry) registerOrGetGauge(name string, dims map[string]string, metricType datapoint.MetricType) *gaugeCollector {
	mr.Lock()
	defer mr.Unlock()

	id := idForMetric(name, dims)
	if c := mr.gauges[id]; c == nil {
		mr.gauges[id] = &gaugeCollector{
			MetricName: name,
			Dimensions: dims,
			Type:       metricType,
		}
	}

	mr.markUsed(id)
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

type access struct {
	ts time.Time
	id metricId
}

// markUsed should be called to indicate that a metricID has been accessed and
// is still in use.  This causes it to move to the front of the lastAccessList
// list with an updated access timestamp.
// The registry lock should be held when calling this method.
func (mr *metricRegistry) markUsed(id metricId) {
	// If this id is new, just push it to the front of the list and put it in
	// our map for quick lookup.
	if _, ok := mr.lastAccesses[id]; !ok {
		elm := mr.lastAccessList.PushFront(&access{
			ts: mr.currentTime(),
			id: id,
		})
		mr.lastAccesses[id] = elm
		return
	}
	// Otherwise, get the element from the map and scoot it up to the front of
	// the list with an updated timestamp.
	elm := mr.lastAccesses[id]
	elm.Value.(*access).ts = mr.currentTime()
	mr.lastAccessList.MoveToFront(elm)
}

func (mr *metricRegistry) purgeOldCollectors() {
	mr.Lock()
	defer mr.Unlock()

	now := mr.currentTime()

	// Start at the back (end) of the linked list (which should always be
	// sorted by last access time) and remove any collectors that haven't been
	// accessed within the expiry timeout.
	elm := mr.lastAccessList.Back()
	for elm != nil {
		acc := elm.Value.(*access)
		if now.Sub(acc.ts) <= mr.expiryTimeout {
			// Since the list is sorted if we reach an element that isn't
			// expired we know no previous elements are expired.
			return
		}

		newElm := elm.Prev()
		// Remove zeros out prev/next so we have to copy previous first
		mr.lastAccessList.Remove(elm)
		elm = newElm

		delete(mr.cumulativeCounters, acc.id)
		delete(mr.gauges, acc.id)
		delete(mr.lastAccesses, acc.id)
	}
}
