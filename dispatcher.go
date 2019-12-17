package main

import (
	"context"

	"github.com/signalfx/golib/v3/datapoint"
	log "github.com/sirupsen/logrus"
)

func (sw *signalfxWriter) sendDatapoints(ctx context.Context, dps []*datapoint.Datapoint) error {
	err := sw.client.AddDatapoints(ctx, dps)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to dispatch datapoints to SignalFx")
		return err
	}

	return nil
}

func (sw *signalfxWriter) shouldDisptach(datapoint *datapoint.Datapoint) bool {
	dispatch := true
	if sw.metricsToExclude[datapoint.Metric] {
		dispatch = false
	}

	for dimKey, dimVal := range datapoint.Dimensions {
		if sw.dimensionPairsToExclude[dimKey] == dimVal {
			dispatch = false
			break
		}
	}

	return dispatch
}
