package main

import (
	"context"
	"time"

	"github.com/signalfx/golib/v3/datapoint"
	log "github.com/sirupsen/logrus"
)

func (listnr *listener) sendDatapoints(ctx context.Context, dps []*datapoint.Datapoint) error {
	err := listnr.client.AddDatapoints(ctx, dps)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to dispatch datapoints to SignalFx")
		return err
	}

	return nil
}

func (listnr *listener) shouldDisptach(datapoint *datapoint.Datapoint) bool {
	dispatch := true
	if listnr.metricsToExclude[datapoint.Metric] {
		dispatch = false
	}

	for dimKey, dimVal := range datapoint.Dimensions {
		if listnr.dimensionPairsToExclude[dimKey] == dimVal {
			dispatch = false
			break
		}
	}

	return dispatch
}

func (listnr *listener) collectDatapointsOnInterval(ticker *time.Ticker) {
	go func() {
		for {
			select {
			case <-ticker.C:
				listnr.dps <- listnr.registry.Datapoints()
			}
		}
	}()
}
