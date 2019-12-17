package internal

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

var defaultConfig = Config{
	Port:            8000,
	IntervalSeconds: 10,
}

type Config struct {
	Port                    int
	Realm                   string
	IngestURL               string
	AccessToken             string
	IntervalSeconds         int
	MetricsToExclude        map[string]bool
	DimensionPairsToExclude map[string]string
	Debug                   bool
	SendInternalMetrics     bool
}

func ConfigFromEnv() *Config {
	c := defaultConfig

	c.AccessToken = os.Getenv("SFX_TOKEN")
	c.IngestURL = os.Getenv("SFX_INGEST_URL")
	c.Realm = os.Getenv("SFX_REALM")

	c.Debug, _ = evaluateBoolEnvVariable(os.Getenv("SFX_DEBUG"), false)
	c.SendInternalMetrics, _ = evaluateBoolEnvVariable(os.Getenv("SFX_INTERNAL_METRICS"), true)

	portEnv := os.Getenv("PORT")
	if portEnv != "" {
		portVal, err := strconv.ParseInt(portEnv, 10, 32)
		if err != nil {
			log.Errorf("Failed to read value from PORT environment variable: %s", err)
		} else {
			c.Port = int(portVal)
		}
	}

	intervalEnvValue := os.Getenv("SFX_REPORTING_INTERVAL")
	if intervalEnvValue != "" {
		n, err := strconv.ParseInt(intervalEnvValue, 10, 32)
		if err != nil {
			log.Errorf("Failed to parse SFX_REPORTING_INTERVAL %q: %v", intervalEnvValue, err)
		} else {
			c.IntervalSeconds = int(n)
		}
	}

	c.MetricsToExclude = getMetricsToExclude(os.Getenv("SFX_METRICS_TO_EXCLUDE"))
	c.DimensionPairsToExclude = getDimensionPairsToExclude(os.Getenv("SFX_DIMENSION_PAIRS_TO_EXCLUDE"))

	return &c
}

func (c *Config) Validate() error {
	if c.AccessToken == "" {
		return fmt.Errorf("SFX_TOKEN environment variable not set")
	}

	if c.Realm == "" && c.IngestURL == "" {
		return errors.New("at least one of SFX_INGEST_URL or SFX_REALM should be set")
	}

	return nil
}

func getMetricsToExclude(metricsToExcludeEnv string) map[string]bool {
	if metricsToExcludeEnv == "" {
		return nil
	}

	metricsToExclude := strings.Split(metricsToExcludeEnv, ",")
	out := makeSetOfStringsFromArray(metricsToExclude)

	return out
}

func getDimensionPairsToExclude(dimensionPairsEnv string) map[string]string {
	if dimensionPairsEnv == "" {
		return nil
	}

	out := make(map[string]string)

	dimensionPairs := strings.Split(dimensionPairsEnv, ",")
	for _, pair := range dimensionPairs {
		splitPair := strings.Split(pair, "=")
		out[splitPair[0]] = splitPair[1]
	}

	log.WithFields(log.Fields{
		"dimension filter": "Dimension key-value pairs to exclude",
	}).Info(out)

	return out
}
