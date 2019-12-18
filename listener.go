package main

import (
	"context"
	"fmt"
	"github.com/signalfx/golib/v3/errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/signalfx/golib/v3/sfxclient"
	log "github.com/sirupsen/logrus"
)

type listener struct {
	scheduler               *sfxclient.Scheduler
	port                    int64
	metricsToExclude        map[string]bool
	dimensionPairsToExclude map[string]string
	registry                *metricRegistry
	ctx                     context.Context
	cancel                  context.CancelFunc
}

func setupListener() (*listener, error) {
	// Output to stderr instead of stdout
	log.SetOutput(os.Stderr)

	// Only log the Info severity or above
	log.SetLevel(log.InfoLevel)

	log.Infoln("Dry run mode on, no datapoints will be emitted to SignalFx")

	logLevel := "info"
	sfxDebug := os.Getenv("SFX_DEBUG")
	isDebug, err := evaluateBoolEnvVariable(sfxDebug, false)

	if err != nil {
		log.WithFields(log.Fields{
			"error":     err,
			"SFX_DEBUG": sfxDebug,
		}).Error("This environment variable supports only boolean values")
	}

	if isDebug {
		log.SetLevel(log.DebugLevel)
		logLevel = "debug"
	}

	log.Infof("Using log level %s", logLevel)

	listnr := &listener{
		scheduler: sfxclient.NewScheduler(),
		port:      8000,
		registry:  newRegistry(),
	}

	// Heroku assigns a port dynamically for an app. 8000 port is used only
	// for testing/developing purposes
	portEnv := os.Getenv("PORT")
	if portEnv != "" {
		port, err := strconv.ParseInt(portEnv, 10, 64)

		if err != nil {
			return nil, fmt.Errorf("failed to read value from PORT environment variable: %s", err)
		}

		listnr.port = port

	}
	sfxToken := os.Getenv("SFX_TOKEN")
	if sfxToken == "" {
		return nil, fmt.Errorf("SFX_TOKEN environment variable not set")
	}

	listnr.scheduler.Sink.(*sfxclient.HTTPSink).AuthToken = sfxToken

	// Prefer SFX_INGEST_URL over SFX_REALM
	if os.Getenv("SFX_INGEST_URL") != "" {
		listnr.scheduler.Sink.(*sfxclient.HTTPSink).DatapointEndpoint = os.Getenv("SFX_INGEST_URL")
	} else if os.Getenv("SFX_REALM") != "" {
		listnr.scheduler.Sink.(*sfxclient.HTTPSink).DatapointEndpoint = fmt.Sprintf("https://ingest.%s.signalfx.com/v2/datapoint", os.Getenv("SFX_REALM"))
	} else {
		return nil, fmt.Errorf("SFX_INGEST_URL or SFX_REALM should be set")
	}

	// Looks for comma-separated metricVal names to exclude. Looks values like the following
	// "metric_name1,metric_name2,metric_name3"
	if os.Getenv("SFX_METRICS_TO_EXCLUDE") != "" {
		metricsToExlcude := strings.Split(os.Getenv("SFX_METRICS_TO_EXCLUDE"), ",")
		listnr.metricsToExclude = makeSetOfStringsFromArray(metricsToExlcude)

		log.WithFields(log.Fields{
			"metricVal filter": "Metrics to exclude",
		}).Info(metricsToExlcude)
	}

	// Looks for dimensions key-value pairs to exclude. Looks values like the following
	// "dim1=val1,dim2=val2,dim3=val3"
	if os.Getenv("SFX_DIMENSION_PAIRS_TO_EXCLUDE") != "" {
		dimensionPairs := strings.Split(os.Getenv("SFX_DIMENSION_PAIRS_TO_EXCLUDE"), ",")
		dims := make(map[string]string)
		for _, pair := range dimensionPairs {
			splitPair := strings.Split(pair, "=")
			dims[splitPair[0]] = splitPair[1]
		}

		log.WithFields(log.Fields{
			"dimension filter": "Dimension key-value pairs to exclude",
		}).Info(dims)

		listnr.dimensionPairsToExclude = dims
	}

	listnr.ctx, listnr.cancel = context.WithCancel(context.Background())
	listnr.scheduler.ReportingDelay(time.Duration(10) * time.Second)
	listnr.scheduler.AddCallback(listnr.registry)

	go func() {
		err := listnr.scheduler.Schedule(listnr.ctx)
		if err != nil {
			if ec, ok := err.(*errors.ErrorChain); !ok || ec.Tail() != context.Canceled {
				log.WithFields(log.Fields{
					"err": err,
				}).Error("Scheduler shutdown unexpectedly")
			}
		}
	}()
	return listnr, nil
}

func main() {
	listnr, err := setupListener()

	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to setup listener")
		return
	}

	http.HandleFunc("/", listnr.processLogs)

	log.WithFields(log.Fields{
		"token":     listnr.scheduler.Sink.(*sfxclient.HTTPSink).AuthToken,
		"ingestURL": listnr.scheduler.Sink.(*sfxclient.HTTPSink).DatapointEndpoint,
		"port":      listnr.port,
	}).Info("Starting up SignalFx Collector")

	err = http.ListenAndServe(fmt.Sprintf(":%d", listnr.port), nil)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to start SignalFx Collector")

		if listnr.cancel != nil {
			listnr.cancel()
		}
	}
}
