package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

type Config struct {
	InfluxDBURL    string
	InfluxDBToken  string
	InfluxDBOrg    string
	InfluxDBBucket string
	ServerPort     string
	Dongle         string
}

type Response struct {
	Generated  float64 `json:"generated"`
	Consumed   float64 `json:"consumed"`
	Exported   float64 `json:"exported"`
	Imported   float64 `json:"imported"`
	Discharged float64 `json:"discharged"`
	MaxPv      float64 `json:"maxPv"`
	Error      string  `json:"error,omitempty"`
}

func loadConfig() (*Config, error) {
	config := &Config{
		InfluxDBURL:    os.Getenv("INFLUXDB_URL"),
		InfluxDBToken:  os.Getenv("INFLUXDB_TOKEN"),
		InfluxDBOrg:    os.Getenv("INFLUXDB_ORG"),
		InfluxDBBucket: os.Getenv("INFLUXDB_BUCKET"),
		ServerPort:     os.Getenv("SERVER_PORT"),
		Dongle:         os.Getenv("DONGLE"),
	}

	if config.ServerPort == "" {
		config.ServerPort = "8080"
	}

	// Validate required configuration
	if config.InfluxDBURL == "" || config.InfluxDBToken == "" ||
		config.InfluxDBOrg == "" || config.InfluxDBBucket == "" ||
		config.Dongle == "" {
		return nil, fmt.Errorf("missing required configuration")
	}

	return config, nil
}

func calculateRangeStart(timeframe string) (time.Time, error) {
	now := time.Now()

	switch timeframe {
	case "day":
		// Get midnight in local time, then convert to UTC
		// Add a 1 minute offset to the local midnight because it seems that the eg4 lags a bit to reset the value to zero.
		localMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, time.Local)
		return localMidnight.UTC(), nil
	case "week":
		return now.AddDate(0, 0, -7).UTC(), nil
	case "month":
		return now.AddDate(0, -1, 0).UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("invalid timeframe: %s", timeframe)
	}
}

func processQueryResult(result *api.QueryTableResult) (float64, error) {
	// Default to 0 if no results
	if !result.Next() {
		return 0, nil
	}

	// Get the first (and only) value
	value := result.Record().Value()
	if value == nil {
		return 0, nil
	}

	// Convert to float64
	floatValue, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected value type: %T", value)
	}

	return floatValue, result.Err()
}

func queryMeasurement(client influxdb2.Client, config *Config, measurement string, start time.Time) (float64, error) {
	queryAPI := client.QueryAPI(config.InfluxDBOrg)

	query := fmt.Sprintf(`
		from(bucket:"%[1]s")
			|> range(start: %[2]s)
			|> filter(fn: (r) => r["_measurement"] == "%[3]s")
			|> filter(fn: (r) => r["_field"] == "value")
			|> filter(fn: (r) => r["dongle"] == "%[4]s")
			|> max()`,
		config.InfluxDBBucket,
		start.Format(time.RFC3339),
		measurement,
		config.Dongle)

	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		return 0, fmt.Errorf("query failed for %s: %v", measurement, err)
	}

	return processQueryResult(result)
}

func queryGenerated(client influxdb2.Client, config *Config, timeframe string) (float64, error) {
	start, err := calculateRangeStart(timeframe)
	if err != nil {
		return 0, err
	}

	measurements := []string{"lux_Epv1_day", "lux_Epv2_day", "lux_Epv3_day"}
	var total float64

	for _, measurement := range measurements {
		value, err := queryMeasurement(client, config, measurement, start)
		if err != nil {
			return 0, err
		}
		total += value
	}

	return total, nil
}

func queryConsumed(client influxdb2.Client, config *Config, timeframe string) (float64, error) {
	start, err := calculateRangeStart(timeframe)
	if err != nil {
		return 0, err
	}

	generated, err := queryGenerated(client, config, timeframe)
	if err != nil {
		return 0, err
	}

	touser, err := queryMeasurement(client, config, "lux_Etouser_day", start)
	if err != nil {
		return 0, err
	}

	dischg, err := queryMeasurement(client, config, "lux_Edischg_day", start)
	if err != nil {
		return 0, err
	}

	togrid, err := queryMeasurement(client, config, "lux_Etogrid_day", start)
	if err != nil {
		return 0, err
	}

	chg, err := queryMeasurement(client, config, "lux_Echg_day", start)
	if err != nil {
		return 0, err
	}

	return generated + touser + dischg - (togrid + chg), nil
}

func queryExported(client influxdb2.Client, config *Config, timeframe string) (float64, error) {
	start, err := calculateRangeStart(timeframe)
	if err != nil {
		return 0, err
	}

	return queryMeasurement(client, config, "lux_Etogrid_day", start)
}

func queryDischarged(client influxdb2.Client, config *Config, timeframe string) (float64, error) {
	start, err := calculateRangeStart(timeframe)
	if err != nil {
		return 0, err
	}

	return queryMeasurement(client, config, "lux_Edischg_day", start)
}

func queryImported(client influxdb2.Client, config *Config, timeframe string) (float64, error) {
	start, err := calculateRangeStart(timeframe)
	if err != nil {
		return 0, err
	}

	return queryMeasurement(client, config, "lux_Etouser_day", start)
}

func queryMaxPv(client influxdb2.Client, config *Config, timeframe string) (float64, error) {
	start, err := calculateRangeStart(timeframe)
	if err != nil {
		return 0, err
	}

	watts, err := queryMeasurement(client, config, "lux_Pall", start)
	if err != nil {
		return 0, err
	}

	return watts / 1000, nil
}

func handleSolarShowdown(client influxdb2.Client, config *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		timeframe := r.URL.Query().Get("timeframe")
		if timeframe == "" {
			timeframe = "day" // Default timeframe
		}

		generated, err := queryGenerated(client, config, timeframe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}

		consumed, err := queryConsumed(client, config, timeframe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}

		exported, err := queryExported(client, config, timeframe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}

		imported, err := queryImported(client, config, timeframe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}

		discharged, err := queryDischarged(client, config, timeframe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}

		maxPv, err := queryMaxPv(client, config, timeframe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{Error: err.Error()})
			return
		}

		response := Response{
			Generated:  generated,
			Consumed:   consumed,
			Exported:   exported,
			Imported:   imported,
			Discharged: discharged,
			MaxPv:      maxPv,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create InfluxDB client
	client := influxdb2.NewClient(config.InfluxDBURL, config.InfluxDBToken)
	defer client.Close()

	// Set up routes
	http.HandleFunc("/solarshowdown", handleSolarShowdown(client, config))

	// Start server
	log.Printf("Starting server on port %s", config.ServerPort)
	if err := http.ListenAndServe(":"+config.ServerPort, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
