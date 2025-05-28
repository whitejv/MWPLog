package aggregation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mwp_data_service/internal/config"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

// NewInfluxDBClient creates and configures a new InfluxDB API client.
func NewInfluxDBClient(cfg config.InfluxDBConfig) (influxdb2.Client, error) {
	client := influxdb2.NewClient(cfg.Host, cfg.Token)
	// Optional: Health check (consider re-enabling with appropriate error handling)
	// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// defer cancel()
	// health, err := client.Health(ctx)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to check InfluxDB health at %s: %w", cfg.Host, err)
	// }
	// if health.Status != influxdb2.HealthCheckStatusPass {
	// 	errMsg := "InfluxDB is not healthy"
	// 	if health.Message != nil {
	// 		errMsg = fmt.Sprintf("%s: %s", errMsg, *health.Message)
	// 	}
	// 	return nil, fmt.Errorf(errMsg)
	// }
	// fmt.Printf("Successfully connected to InfluxDB at %s\n", cfg.Host)
	return client, nil
}

// AggregatedRecord holds the data retrieved from InfluxDB for a single controller/zone.
type AggregatedRecord struct {
	Controller   string  `json:"controller"`
	Zone         string  `json:"zone"`
	TotalFlow    float64 `json:"totalFlow"`    // Sum of intervalFlow
	TotalSeconds float64 `json:"totalSeconds"` // Sum of secondsOn
	AvgPSI       float64 `json:"avgPSI"`       // Mean of pressurePSI
	AvgTempF     float64 `json:"avgTempF"`     // Mean of temperatureF
	AvgAmps      float64 `json:"avgAmps"`      // Mean of amperage
}

// Helper function to parse month names to time.Month
func parseMonth(monthStr string) (time.Month, error) {
	switch strings.ToLower(monthStr) {
	case "january":
		return time.January, nil
	case "february":
		return time.February, nil
	case "march":
		return time.March, nil
	case "april":
		return time.April, nil
	case "may":
		return time.May, nil
	case "june":
		return time.June, nil
	case "july":
		return time.July, nil
	case "august":
		return time.August, nil
	case "september":
		return time.September, nil
	case "october":
		return time.October, nil
	case "november":
		return time.November, nil
	case "december":
		return time.December, nil
	default:
		return 0, fmt.Errorf("invalid month: %s", monthStr)
	}
}

// isYear checks if a string is a four-digit number, likely a year.
func isYear(s string) (int, bool) {
	if len(s) != 4 {
		return 0, false
	}
	year, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	// Basic sanity check for a reasonable year range, adjust if needed.
	if year < 1900 || year > 2200 {
		return 0, false
	}
	return year, true
}

// QueryAggregatedData constructs and executes a Flux query to get aggregated data.
// The timeRange parameter can be a relative duration (e.g., "24h"), "now", a month name (e.g., "June"), or a year (e.g., "2023").
func QueryAggregatedData(client influxdb2.Client, timeRange string, dbConfig config.InfluxDBConfig) ([]AggregatedRecord, error) {
	queryAPI := client.QueryAPI(dbConfig.Org)

	fluxRangeParameter := ""

	if strings.ToLower(timeRange) == "now" {
		fluxRangeParameter = "start: -1m" // "now" means query the last 1 minute
	} else if yearVal, isYr := isYear(timeRange); isYr {
		// Handle year
		startOfYear := time.Date(yearVal, time.January, 1, 0, 0, 0, 0, time.UTC)
		endOfYear := startOfYear.AddDate(1, 0, 0) // First day of the next year
		fluxRangeParameter = fmt.Sprintf("start: %s, stop: %s",
			startOfYear.Format(time.RFC3339),
			endOfYear.Format(time.RFC3339))
	} else if month, err := parseMonth(timeRange); err == nil {
		// Handle month name
		currentTime := time.Now() // Use current time to determine the year for the month
		startOfMonth := time.Date(currentTime.Year(), month, 1, 0, 0, 0, 0, time.UTC)
		endOfMonth := startOfMonth.AddDate(0, 1, 0) // First day of the next month

		fluxRangeParameter = fmt.Sprintf("start: %s, stop: %s",
			startOfMonth.Format(time.RFC3339),
			endOfMonth.Format(time.RFC3339))
	} else {
		// Assume it's a relative duration string like "24h", "7d"
		// Ensure it starts with a '-' for Flux relative ranges if it's a duration.
		// If it's already correctly formatted (e.g. "-24h" or part of an absolute range string from elsewhere), this might need adjustment.
		// For now, we assume simple durations that need a leading '-'.
		if !strings.HasPrefix(timeRange, "-") && !strings.Contains(timeRange, ":") { // Avoid double-prefixing or altering absolute ranges
			fluxRangeParameter = "start: -" + timeRange
		} else {
			// It might be an already correctly formatted relative range like "-24h"
			// or an absolute range string like "start: ..., stop: ..."
			// If it contains ":" it's likely part of an absolute range or already formatted.
			if strings.Contains(timeRange, ":") {
				fluxRangeParameter = timeRange // Assume it's a fully formed range string part
			} else {
				fluxRangeParameter = "start: " + timeRange // e.g. start: -24h
			}
		}
	}

	fluxQuery := fmt.Sprintf(`
import "timezone"
option location = timezone.location(name: "America/Chicago")

// Define a common filter function for base data selection
filterBase = (tables=<-, fieldFilter) =>
  tables
    |> filter(fn: (r) => r._measurement == "mwp_sensors")
    |> filter(fn: (r) => exists r.Controller and r.Controller != "" and exists r.Zone and r.Zone != "")
    |> filter(fn: fieldFilter)

// Calculate sums for specific fields
sums = from(bucket: "%[1]s")
  |> range(%[2]s) // Use the processed fluxRangeParameter
  |> filterBase(fieldFilter: (r) => r._field == "intervalFlow" or r._field == "secondsOn")
  |> group(columns: ["Controller", "Zone", "_field"])
  |> sum()
  |> group(columns: ["Controller", "Zone"])

// Calculate means for specific fields
means = from(bucket: "%[1]s")
  |> range(%[2]s) // Use the processed fluxRangeParameter
  |> filterBase(fieldFilter: (r) => r._field == "pressurePSI" or r._field == "temperatureF" or r._field == "amperage")
  |> group(columns: ["Controller", "Zone", "_field"])
  |> mean()
  |> group(columns: ["Controller", "Zone"])

// Union the sum and mean results, then pivot
union(tables: [sums, means])
  |> pivot(rowKey:["Controller", "Zone"], columnKey: ["_field"], valueColumn: "_value")
  |> map(fn: (r) => ({
      Controller: r.Controller,
      Zone: r.Zone,
      totalFlow: if exists r.intervalFlow then r.intervalFlow else 0.0,
      totalSeconds: if exists r.secondsOn then r.secondsOn else 0.0,
      avgPSI: if exists r.pressurePSI then r.pressurePSI else 0.0,
      avgTempF: if exists r.temperatureF then r.temperatureF else 0.0,
      avgAmps: if exists r.amperage then r.amperage else 0.0
  }))
  |> yield(name: "results")
`, dbConfig.Bucket, fluxRangeParameter)

	fmt.Printf("Executing InfluxDB Query:\n%s\n", fluxQuery)

	result, err := queryAPI.Query(context.Background(), fluxQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute InfluxDB query: %w\nQuery Sent:\n%s", err, fluxQuery)
	}
	defer result.Close()

	var records []AggregatedRecord
	for result.Next() {
		if result.Err() != nil {
			return nil, fmt.Errorf("error processing query result row: %w", result.Err())
		}
		fluxRecord := result.Record()

		// Helper function to safely get float64 values from the record
		getFloat := func(fieldName string) float64 {
			val, ok := fluxRecord.Values()[fieldName]
			if !ok || val == nil {
				// Field might be legitimately missing if no data for that specific metric (e.g. no amperage readings)
				// fmt.Printf("Trace: Field '%s' not found or nil in result record for C:%s, Z:%s\n",
				// 	fieldName, fluxRecord.ValueByKey("Controller"), fluxRecord.ValueByKey("Zone"))
				return 0.0
			}
			if floatVal, typeOk := val.(float64); typeOk {
				return floatVal
			}
			if intVal, typeOk := val.(int64); typeOk {
				return float64(intVal)
			}
			fmt.Printf("Warning: Could not convert field '%s' to float64 for C:%s Z:%s, value: %v, type: %T\n",
				fieldName, fluxRecord.ValueByKey("Controller"), fluxRecord.ValueByKey("Zone"), val, val)
			return 0.0
		}

		// Helper function to safely get string values from the record
		getString := func(fieldName string) string {
			val, ok := fluxRecord.Values()[fieldName]
			if !ok || val == nil {
				// This should ideally not happen for Controller and Zone due to query filters and pivot
				fmt.Printf("Warning: Required field '%s' not found or nil in result record.\n", fieldName)
				return ""
			}
			if strVal, typeOk := val.(string); typeOk {
				return strVal
			}
			fmt.Printf("Warning: Could not convert field '%s' to string, value: %v, type: %T\n", fieldName, val, val)
			return ""
		}

		aggRecord := AggregatedRecord{
			Controller:   getString("Controller"),
			Zone:         getString("Zone"),
			TotalFlow:    getFloat("totalFlow"),    // Mapped from r.intervalFlow if pivot worked as expected
			TotalSeconds: getFloat("totalSeconds"), // Mapped from r.secondsOn
			AvgPSI:       getFloat("avgPSI"),       // Mapped from r.pressurePSI
			AvgTempF:     getFloat("avgTempF"),     // Mapped from r.temperatureF
			AvgAmps:      getFloat("avgAmps"),      // Mapped from r.amperage
		}
		records = append(records, aggRecord)
	}

	if result.Err() != nil { // Check for any final error from result iteration
		return nil, fmt.Errorf("error iterating InfluxDB query results: %w", result.Err())
	}

	fmt.Printf("InfluxDB query successful, %d aggregated records retrieved.\n", len(records))
	return records, nil
}
