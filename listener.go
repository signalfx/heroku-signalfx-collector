package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/signalfx/golib/v3/datapoint"
	"github.com/signalfx/golib/v3/sfxclient"
	sfxwriter "github.com/signalfx/signalfx-go/writer"
	log "github.com/sirupsen/logrus"
)

type signalfxWriter struct {
	client                  *sfxclient.HTTPSink
	datapointWriter         *sfxwriter.DatapointWriter
	dps                     chan []*datapoint.Datapoint
	ingestUrl               string
	port                    string
	metricsToExclude        map[string]bool
	dimensionPairsToExclude map[string]string
}

const IngestUrl = "https://ingest.signalfx.com/v2/datapoint"

func init() {
	// Output to stderr instead of stdout
	log.SetOutput(os.Stderr)

	// Only log the Info severity or above
	log.SetLevel(log.InfoLevel)

	sfxDebug := os.Getenv("SFX_DEBUG")
	logLevel := "info"
	if sfxDebug != "" {
		isDebug := evaluateBoolEnvVariable("SFX_DEBUG", sfxDebug, false)

		if isDebug {
			log.SetLevel(log.DebugLevel)
			logLevel = "debug"
		}
	}

	log.Infof("Using log level %s", logLevel)
}

func setupAndStartSignalFxWriter() *signalfxWriter {
	sw := &signalfxWriter{
		dps:       make(chan []*datapoint.Datapoint, 1),
		client:    sfxclient.NewHTTPSink(),
		ingestUrl: IngestUrl,
		port:      "8080",
	}

	// Heroku assigns a port dynamically for an app. This port is used only
	// for testing/developing purposes
	if os.Getenv("PORT") != "" {
		sw.port = os.Getenv("PORT")
	}

	if os.Getenv("SFX_TOKEN") != "" {
		sw.client.AuthToken = os.Getenv("SFX_TOKEN")
	}

	// Prefer SFX_INGEST_URL over SFX_REALM
	if os.Getenv("SFX_INGEST_URL") != "" {
		sw.client.DatapointEndpoint = os.Getenv("SFX_INGEST_URL")
	} else if os.Getenv("SFX_REALM") != "" {
		sw.client.DatapointEndpoint = IngestUrl[0:14] + os.Getenv("SFX_REALM") + IngestUrl[13:]
	}

	// Looks for comma-separated metricVal names to exclude. Looks values like the following
	// "metric_name1,metric_name2,metric_name3"
	if os.Getenv("SFX_METRICS_TO_EXCLUDE") != "" {
		metricsToExlcude := strings.Split(os.Getenv("SFX_METRICS_TO_EXCLUDE"), ",")
		sw.metricsToExclude = makeSetOfStringsFromArray(metricsToExlcude)

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

		sw.dimensionPairsToExclude = dims
	}

	sw.datapointWriter = &sfxwriter.DatapointWriter{
		PreprocessFunc: sw.shouldDisptach,
		SendFunc:       sw.sendDatapoints,
		InputChan:      sw.dps,
	}

	ctx, _ := context.WithCancel(context.Background())
	sw.datapointWriter.Start(ctx)

	return sw
}

func main() {
	sw := setupAndStartSignalFxWriter()
	http.HandleFunc("/", sw.processLogs)

	log.WithFields(log.Fields{
		"ingestURL": sw.ingestUrl,
		"port":      sw.port,
	}).Info("Starting up SignalFx Collector")

	err := http.ListenAndServe(":"+sw.port, nil)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("Failed to start SignalFx Collector")
	}
}
