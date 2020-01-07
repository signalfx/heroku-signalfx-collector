package internal

import (
	"bufio"
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
	"github.com/signalfx/heroku-signalfx-collector/internal/registry"
	log "github.com/sirupsen/logrus"
)

type Listener struct {
	dps                     chan<- []*datapoint.Datapoint
	metricsToExclude        map[string]bool
	dimensionPairsToExclude map[string]string
	registry                *registry.MetricRegistry
	intervalSeconds         int
	totalRequests           int64

	ctx    context.Context
	cancel context.CancelFunc
}

func NewListener(intervalSeconds int, dpChan chan<- []*datapoint.Datapoint) (*Listener, error) {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{
		dps:             dpChan,
		registry:        registry.New(5 * time.Minute),
		ctx:             ctx,
		cancel:          cancel,
		intervalSeconds: intervalSeconds,
	}

	return l, nil
}

func (l *Listener) Start() error {
	log.WithFields(log.Fields{
		"intervalSeconds": l.intervalSeconds,
	}).Info("Setting up datapoint collector")

	go func() {
		ticker := time.NewTicker(time.Duration(l.intervalSeconds) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				dps := l.registry.Datapoints()

				// Use n + loop to shift up valid datapoints in the slice
				n := 0
				for i := range dps {
					if l.shouldDispatch(dps[i]) {
						dps[n] = dps[i]
						n++
					}
				}

				l.dps <- dps[:n]
			case <-l.ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (l *Listener) shouldDispatch(datapoint *datapoint.Datapoint) bool {
	if l.metricsToExclude[datapoint.Metric] {
		return false
	}

	for dimKey, dimVal := range datapoint.Dimensions {
		if l.dimensionPairsToExclude[dimKey] == dimVal {
			return false
		}
	}

	return true
}

func (l *Listener) ProcessLogs(w http.ResponseWriter, req *http.Request) {
	atomic.AddInt64(&l.totalRequests, 1)

	dims, err := getDimensionParisFromParams(req.URL.Query())

	if err != nil {
		log.WithFields(log.Fields{
			"error":  err,
			"params": req.URL.Query().Encode(),
		}).Error("Unable to get App name from request param (appname)")

		return
	}

	scanner := bufio.NewScanner(req.Body)
	for scanner.Scan() {
		line := scanner.Text()

		processedLog, err := detectAndParseLog(line)

		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"line":  line,
			}).Error("Error processing supported log line")

			continue
		}

		if processedLog != nil {
			metrics, dims := processMetrics(processedLog, dims)
			l.registry.UpdateMetrics(metrics, dims)
		}
	}
}

func (l *Listener) InternalMetrics() []*datapoint.Datapoint {
	return append(l.registry.InternalMetrics(), []*datapoint.Datapoint{
		sfxclient.CumulativeP("sfx_heroku.total_drain_requests", nil, &l.totalRequests),
	}...)
}

func (l *Listener) Shutdown() {
	if l.cancel != nil {
		l.cancel()
	}
}
