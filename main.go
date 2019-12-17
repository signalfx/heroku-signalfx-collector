package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
	"github.com/signalfx/heroku-signalfx-collector/internal"
	sfxwriter "github.com/signalfx/signalfx-go/writer"
)

func main() {
	conf := internal.ConfigFromEnv()
	if err := conf.Validate(); err != nil {
		log.WithError(err).Error("Config was invalid")
		os.Exit(1)
	}

	setupLogging(conf)

	client := makeClient(conf)

	dpChan := make(chan []*datapoint.Datapoint, 1)

	datapointWriter := &sfxwriter.DatapointWriter{
		SendFunc: func(ctx context.Context, dps []*datapoint.Datapoint) error {
			err := client.AddDatapoints(ctx, dps)
			if err != nil {
				log.WithFields(log.Fields{
					"error": err,
				}).Error("Failed to dispatch datapoints to SignalFx")
				return err
			}

			return nil
		},
		InputChan: dpChan,
	}

	datapointWriter.Start(context.Background())

	listener, err := internal.NewListener(conf.IntervalSeconds, dpChan)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to setup listener")
		os.Exit(2)
	}

	if err := listener.Start(); err != nil {
		log.WithError(err).Error("Failed to start listener")
		os.Exit(3)
	}

	http.HandleFunc("/", listener.ProcessLogs)

	if conf.SendInternalMetrics {
		log.Infof("Sending internal metrics")

		go sendInternalMetrics(conf.IntervalSeconds, dpChan, listener)
	}

	err = http.ListenAndServe(fmt.Sprintf(":%d", conf.Port), nil)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to start SignalFx Collector")
		os.Exit(4)
	}

	log.Infoln("Shutting Down")
}

func setupLogging(conf *internal.Config) {
	// Output to stderr instead of stdout
	log.SetOutput(os.Stderr)

	// Only log the Info severity or above
	log.SetLevel(log.InfoLevel)

	if conf.Debug {
		log.SetLevel(log.DebugLevel)
	}
}

func makeClient(conf *internal.Config) *sfxclient.HTTPSink {
	client := sfxclient.NewHTTPSink()
	client.AuthToken = conf.AccessToken

	// Prefer SFX_INGEST_URL over SFX_REALM
	switch {
	case conf.IngestURL != "":
		client.DatapointEndpoint = fmt.Sprintf("%s/v2/datapoint", conf.IngestURL)
	case conf.Realm != "":
		client.DatapointEndpoint = fmt.Sprintf("https://ingest.%s.signalfx.com/v2/datapoint", conf.Realm)
	default:
		panic("ingest URL or realm should be set")
	}

	log.Infof("Sending datapoints to %s", client.DatapointEndpoint)

	return client
}

func sendInternalMetrics(intervalSeconds int, dpChan chan<- []*datapoint.Datapoint, listener *internal.Listener) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dps := listener.InternalMetrics()

			for i := range dps {
				if dps[i].Dimensions == nil {
					dps[i].Dimensions = map[string]string{}
				}

				dps[i].Dimensions["heroku_app"] = os.Getenv("HEROKU_APP_NAME")
				dps[i].Dimensions["dyno_id"] = os.Getenv("HEROKU_DYNO_ID")
				dps[i].Dimensions["source"] = "signalfx-heroku-collector"
			}

			dpChan <- dps
		case <-context.Background().Done():
			return
		}
	}
}
