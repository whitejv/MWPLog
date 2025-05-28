package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

const (
	influxDBHost   = "http://192.168.1.250:8086"
	influxDBToken  = "RHl3fYEp8eMLtIUraVPzY4zp_hnnu2kYlR9hYrUaJLcq5mB2PvDsOi9SR0Tu_i-t_183fHb1a95BTJug-vAPVQ=="
	influxDBOrg    = "Milano"
	influxDBBucket = "MWPWater"
)

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

// buildQuery now accepts queryRangeStr which can be a relative duration or absolute start/stop times
func buildQuery(bucket, queryRangeStr, aggregateWindow, controller, zone string) string {
	var filterStr string
	if zone != "" {
		filterStr = fmt.Sprintf(`|> filter(fn: (r) => r["Controller"] == "%s" and r["Zone"] == "%s")`, controller, zone)
	} else {
		filterStr = fmt.Sprintf(`|> filter(fn: (r) => r["Controller"] == "%s")`, controller)
	}

	// Determine if queryRangeStr is for absolute or relative range
	rangeOperator := ""
	if strings.Contains(queryRangeStr, "start:") { // Indicates an absolute range string
		rangeOperator = queryRangeStr // Use as is: "start: 2023-01-01T00:00:00Z, stop: 2023-02-01T00:00:00Z"
	} else { // Assumed to be a relative duration like "24h"
		rangeOperator = fmt.Sprintf("start: -%s", queryRangeStr)
	}

	return fmt.Sprintf(`
		import "timezone"
		option location = timezone.location(name: "America/Chicago")

        // First query: Get summed intervalFlow
        intervalFlow = from(bucket: "%s")
            |> range(%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "intervalFlow")
            %s
            |> aggregateWindow(every: %s, fn: sum, createEmpty: false)
            |> yield(name: "intervalFlow")

        // Second query: Get summed secondsOn
        secondsOn = from(bucket: "%s")
            |> range(%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "secondsOn")
            %s
            |> aggregateWindow(every: %s, fn: sum, createEmpty: false)
            |> yield(name: "secondsOn")

        // Third query: Get averaged pressurePSI
        pressure = from(bucket: "%s")
            |> range(%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "pressurePSI")
            %s
            |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
            |> yield(name: "pressure")

        // Fourth query: Get averaged temperatureF
        temperature = from(bucket: "%s")
            |> range(%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "temperatureF")
            %s
            |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
            |> yield(name: "temperature")

        // Fifth query: Get averaged amperage
        amperage = from(bucket: "%s")
            |> range(%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "amperage")
            %s
            |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
            |> yield(name: "amperage")
    `, bucket, rangeOperator, filterStr, aggregateWindow,
		bucket, rangeOperator, filterStr, aggregateWindow,
		bucket, rangeOperator, filterStr, aggregateWindow,
		bucket, rangeOperator, filterStr, aggregateWindow,
		bucket, rangeOperator, filterStr, aggregateWindow)
}

func main() {
	// Load the America/Chicago time zone
	centralLoc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		fmt.Printf("Error loading timezone: %s\n", err)
		os.Exit(1)
	}

	// Command line flags with short and long versions
	var windowFlag string
	var rangeFlag string
	var controllerFlag string
	var zoneFlag string

	// Long versions
	flag.StringVar(&controllerFlag, "controller", "1", "Controller number (0-5)")
	flag.StringVar(&controllerFlag, "c", "1", "Controller number (shorthand)")

	flag.StringVar(&rangeFlag, "range", "24h", `Time range to query. Valid options:
        1. Duration string: e.g., "30m", "1h", "2d", "48h".
           This defines a period relative to the current time (e.g., "24h" means the last 24 hours).
        2. "now": Displays data from the last 1 minute.
        3. Month name: e.g., "January", "February", ..., "December".
           Queries data for the entire specified calendar month of the current year.`)
	flag.StringVar(&rangeFlag, "r", "24h", "Time range to query (shorthand)")

	flag.StringVar(&windowFlag, "window", "", `Aggregation window for the data (e.g., "1s", "1m", "5m", "1h", "1d").
        - If --range is a duration string (e.g., "24h") and --window is not set, --window defaults to the --range value.
        - If --range is "now" and --window is not set, --window defaults to "1s".
        - If --range is a month name (e.g., "June") and --window is not set, --window defaults to "1d".
        You can always override these default aggregation windows by explicitly setting the --window value.`)
	flag.StringVar(&windowFlag, "w", "", "Aggregation window for the data (shorthand)")

	flag.StringVar(&zoneFlag, "zone", "", "Zone number (0-16). If not specified, shows all zones for the selected controller.")
	flag.StringVar(&zoneFlag, "z", "", "Zone number (shorthand)")

	flag.Parse()

	queryRangeStr := rangeFlag      // This will be passed to buildQuery. Can be relative duration or absolute start/stop.
	actualRangeDisplay := rangeFlag // For display purposes

	// Handle "now" keyword for range
	if strings.ToLower(rangeFlag) == "now" {
		queryRangeStr = "1m" // Query last 1 minute
		actualRangeDisplay = "now (last 1 minute)"
		if windowFlag == "" {
			windowFlag = "1s" // Default aggregation window for "now"
		}
	} else if month, err := parseMonth(rangeFlag); err == nil {
		// Handle month name for range
		now := time.Now()
		// Construct start and end times for the month in UTC for the query
		// The InfluxDB timezone option will handle display in Central time.
		startOfMonth := time.Date(now.Year(), month, 1, 0, 0, 0, 0, time.UTC)
		endOfMonth := startOfMonth.AddDate(0, 1, 0) // First day of next month

		queryRangeStr = fmt.Sprintf("start: %s, stop: %s",
			startOfMonth.Format(time.RFC3339),
			endOfMonth.Format(time.RFC3339))
		actualRangeDisplay = fmt.Sprintf("%s %d", month.String(), now.Year())
		if windowFlag == "" {
			windowFlag = "1d" // Default aggregation window for month queries
		}
	}

	// If windowFlag is still empty (not "now", not a month, and not explicitly set), default it.
	// For simple duration ranges, if window is not set, it defaults to the range itself
	// (resulting in one aggregated point).
	if windowFlag == "" {
		// Check if rangeFlag is a simple duration (not "now" and not a month that was processed above)
		_, monthParseErr := parseMonth(rangeFlag)
		if strings.ToLower(rangeFlag) != "now" && monthParseErr != nil {
			windowFlag = rangeFlag
		} else if windowFlag == "" { // Catch-all if still not set (e.g. if range was 'now' or a month but window not set by those blocks)
			// This case might be redundant if "now" and month logic correctly sets windowFlag
			// but as a fallback, default to range.
			windowFlag = queryRangeStr // or rangeFlag, depending on desired default for complex cases
		}
	}

	// Basic validation for windowFlag - check if it's a duration or one of the special values handled.
	// More robust duration string validation could be added if needed.
	// For now, rely on InfluxDB to reject invalid duration strings.
	if windowFlag == "" {
		fmt.Println("Error: Aggregation window (-w, --window) could not be determined. Please specify a valid window.")
		os.Exit(1)
	}

	// Validate controller
	controllerNum, err := strconv.Atoi(controllerFlag)
	if err != nil || controllerNum < 0 || controllerNum > 5 {
		fmt.Println("Invalid controller. Must be between 0 and 5")
		os.Exit(1)
	}

	// Validate zone only if specified
	if zoneFlag != "" {
		zoneNum, err := strconv.Atoi(zoneFlag)
		if err != nil || zoneNum < 0 || zoneNum > 16 {
			fmt.Println("Invalid zone. Must be between 0 and 16")
			os.Exit(1)
		}
	}

	// Configure client
	client := influxdb2.NewClient(influxDBHost, influxDBToken)
	defer client.Close()

	// Get query client
	queryAPI := client.QueryAPI(influxDBOrg)

	// Build and execute query
	query := buildQuery(influxDBBucket, queryRangeStr, windowFlag, controllerFlag, zoneFlag)
	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		fmt.Printf("Query error: %s\nQuery was:\n%s\n", err, query)
		os.Exit(1)
	}

	// Process results
	fmt.Printf("\nController: %s, Zone: %s\n", controllerFlag, zoneFlag)
	fmt.Printf("Time Window (Aggregation): %s, Range: %s\n\n", windowFlag, actualRangeDisplay) // Use actualRangeDisplay

	// Create a map to store the latest values for each timestamp
	type TimeData struct {
		Time         time.Time
		IntervalFlow float64
		SecondsOn    float64 // Added SecondsOn
		PressurePSI  float64
		Temperature  float64
		Amperage     float64
	}
	timeMap := make(map[string]TimeData)

	for result.Next() {
		record := result.Record()
		// Convert UTC time to Central time using the timezone location
		utcTime := record.Time()
		localTime := utcTime.In(centralLoc)
		timeStr := localTime.Format("2006-01-02 15:04:05") // Use standard format for map key

		data, exists := timeMap[timeStr]
		if !exists {
			data = TimeData{Time: localTime} // Store local time
		}

		// Update the appropriate field based on the measurement name/field
		switch record.Field() {
		case "intervalFlow":
			if v, ok := record.Value().(float64); ok {
				data.IntervalFlow = v
			}
		case "secondsOn": // Added case for secondsOn
			if v, ok := record.Value().(float64); ok {
				data.SecondsOn = v
			}
		case "pressurePSI":
			if v, ok := record.Value().(float64); ok {
				data.PressurePSI = v
			}
		case "temperatureF":
			if v, ok := record.Value().(float64); ok {
				data.Temperature = v
			}
		case "amperage":
			if v, ok := record.Value().(float64); ok {
				data.Amperage = v
			}
		}

		timeMap[timeStr] = data
	}

	if result.Err() != nil {
		fmt.Printf("Query processing error: %s\n", result.Err())
		os.Exit(1)
	}

	// Check if any data was collected
	if len(timeMap) == 0 {
		fmt.Println("No data found for the specified criteria.")
		return
	}

	// Print the header
	fmt.Printf("%-19s %12s %10s %10s %10s %8s\n",
		"Time (Central)", "Flow", "Seconds", "PSI", "Temp°F", "Amps") // Added Seconds column
	fmt.Println(strings.Repeat("-", 75)) // Adjusted header line length

	// Sort the timestamps
	times := make([]string, 0, len(timeMap))
	for t := range timeMap {
		times = append(times, t)
	}
	sort.Strings(times)

	// Print the data in chronological order
	for _, t := range times {
		data := timeMap[t]
		fmt.Printf("%-19s %12.2f %10.0f %10.1f %10.1f %8.2f\n",
			t, // Use the formatted time string key
			data.IntervalFlow,
			data.SecondsOn, // Added SecondsOn data
			data.PressurePSI,
			data.Temperature,
			data.Amperage)
	}
}
