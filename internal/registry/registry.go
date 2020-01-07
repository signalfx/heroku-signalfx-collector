package registry

import (
	"container/list"
	"sort"
	"sync"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
	log "github.com/sirupsen/logrus"
)

type metricID string

// Keeps track of all the metrics that have been reporting
type MetricRegistry struct {
	sync.RWMutex

	cumulativeCounters map[metricID]*CumulativeCollector
	gauges             map[metricID]*GaugeCollector
	counters           map[metricID]*CounterCollector

	// A linked list that we keep sorted by access time so that we can very
	// quickly tell which collectors are expired and should be deleted.
	lastAccessList list.List
	// A map optimizing lookup of the access time elements that are used in the
	// above linked list.
	lastAccesses map[metricID]*list.Element

	expiryTimeout time.Duration

	// This is the source of truth for the current time and exists to make unit
	// testing easier
	currentTime func() time.Time
}

func (mr *MetricRegistry) Datapoints() []*datapoint.Datapoint {
	mr.purgeOldCollectors()

	mr.RLock()

	var out []*datapoint.Datapoint

	for id := range mr.gauges {
		out = append(out, mr.gauges[id].Datapoints()...)
	}

	for id := range mr.cumulativeCounters {
		out = append(out, mr.cumulativeCounters[id].Datapoints()...)
	}

	for id := range mr.counters {
		out = append(out, mr.counters[id].Datapoints()...)
	}

	mr.RUnlock()

	return out
}

func (mr *MetricRegistry) InternalMetrics() []*datapoint.Datapoint {
	mr.RLock()
	defer mr.RUnlock()

	return []*datapoint.Datapoint{
		sfxclient.Gauge("sfx_heroku.tracked_metrics", map[string]string{"type": "cumulative_counter"}, int64(len(mr.cumulativeCounters))),
		sfxclient.Gauge("sfx_heroku.tracked_metrics", map[string]string{"type": "gauge"}, int64(len(mr.gauges))),
		sfxclient.Gauge("sfx_heroku.tracked_metrics", map[string]string{"type": "counter"}, int64(len(mr.counters))),
	}
}

type MetricVal struct {
	Name  string
	Type  datapoint.MetricType
	Value float64
}

var _ sfxclient.Collector = &MetricRegistry{}

func New(expiryTimeout time.Duration) *MetricRegistry {
	return &MetricRegistry{
		cumulativeCounters: map[metricID]*CumulativeCollector{},
		gauges:             map[metricID]*GaugeCollector{},
		counters:           map[metricID]*CounterCollector{},
		lastAccesses:       make(map[metricID]*list.Element),
		expiryTimeout:      expiryTimeout,
		currentTime:        time.Now,
	}
}

func (mr *MetricRegistry) UpdateMetrics(mvs []*MetricVal, dims map[string]string) {
	for _, mv := range mvs {
		mr.UpdateMetric(mv, dims)
	}
}

func (mr *MetricRegistry) UpdateMetric(mv *MetricVal, dims map[string]string) {
	mr.Lock()
	defer mr.Unlock()

	id := idForMetric(mv.Name, dims)

	switch mv.Type {
	case datapoint.Gauge:
		if c := mr.gauges[id]; c == nil {
			mr.gauges[id] = &GaugeCollector{
				MetricName: mv.Name,
				Dimensions: dims,
			}
		}

		mr.gauges[id].Set(mv.Value)
	case datapoint.Count:
		if c := mr.counters[id]; c == nil {
			mr.counters[id] = &CounterCollector{
				MetricName: mv.Name,
				Dimensions: dims,
			}
		}

		mr.counters[id].Add(mv.Value)
	case datapoint.Counter:
		if c := mr.cumulativeCounters[id]; c == nil {
			mr.cumulativeCounters[id] = &CumulativeCollector{
				MetricName: mv.Name,
				Dimensions: dims,
			}
		}

		mr.cumulativeCounters[id].Add(mv.Value)
	default:
		log.WithFields(log.Fields{
			"metric": mv.Name,
			"type":   mv.Type,
		}).Warn("Unsupported metric type")
	}

	mr.markUsed(id)
}

func idForMetric(name string, dims map[string]string) metricID {
	id := name + "|"

	for _, key := range sortKeys(dims) {
		id += key + ":" + dims[key] + "|"
	}

	return metricID(id)
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
	id metricID
}

// markUsed should be called to indicate that a metricID has been accessed and
// is still in use.  This causes it to move to the front of the lastAccessList
// list with an updated access timestamp.
// The registry lock should be held when calling this method.
func (mr *MetricRegistry) markUsed(id metricID) {
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

func (mr *MetricRegistry) purgeOldCollectors() {
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
		delete(mr.counters, acc.id)
		delete(mr.lastAccesses, acc.id)
	}
}
