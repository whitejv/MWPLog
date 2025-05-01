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

var timeWindows = map[string]string{
	"1m":   "1m",
	"5m":   "5m",
	"1h":   "1h",
	"12h":  "12h",
	"24h":  "24h",
	"7d":   "168h",
	"30d":  "720h",
	"3mo":  "2160h",
	"6mo":  "4320h",
	"12mo": "8760h",
}

func buildQuery(bucket, timeRange, aggregateWindow, controller, zone string) string {
	// Build the zone filter based on whether it was specified
	var filterStr string
	if zone != "" {
		filterStr = fmt.Sprintf(`|> filter(fn: (r) => r["Controller"] == "%s" and r["Zone"] == "%s")`, controller, zone)
	} else {
		filterStr = fmt.Sprintf(`|> filter(fn: (r) => r["Controller"] == "%s")`, controller)
	}

	return fmt.Sprintf(`
        // First query: Get summed intervalFlow
        intervalFlow = from(bucket: "%s")
            |> range(start: -%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "intervalFlow")
            %s
            |> aggregateWindow(every: %s, fn: sum, createEmpty: false)
            |> yield(name: "intervalFlow")

        // Second query: Get summed secondsOn
        secondsOn = from(bucket: "%s")
            |> range(start: -%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "secondsOn")
            %s
            |> aggregateWindow(every: %s, fn: sum, createEmpty: false)
            |> yield(name: "secondsOn")

        // Third query: Get averaged pressurePSI
        pressure = from(bucket: "%s")
            |> range(start: -%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "pressurePSI")
            %s
            |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
            |> yield(name: "pressure")

        // Fourth query: Get averaged temperatureF
        temperature = from(bucket: "%s")
            |> range(start: -%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "temperatureF")
            %s
            |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
            |> yield(name: "temperature")

        // Fifth query: Get averaged amperage
        amperage = from(bucket: "%s")
            |> range(start: -%s)
            |> filter(fn: (r) => r["_measurement"] == "mwp_sensors")
            |> filter(fn: (r) => r["_field"] == "amperage")
            %s
            |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
            |> yield(name: "amperage")
    `, bucket, timeRange, filterStr, aggregateWindow,
		bucket, timeRange, filterStr, aggregateWindow,
		bucket, timeRange, filterStr, aggregateWindow,
		bucket, timeRange, filterStr, aggregateWindow,
		bucket, timeRange, filterStr, aggregateWindow)
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
	flag.StringVar(&windowFlag, "window", "", "Time window for aggregation (1m, 5m, 1h, 12h, 24h, 7d, 30d, 3mo, 6mo, 12mo)")
	flag.StringVar(&rangeFlag, "range", "24h", "Time range to query (same options as window)")
	flag.StringVar(&controllerFlag, "controller", "1", "Controller number (0-5)")
	flag.StringVar(&zoneFlag, "zone", "", "Zone number (0-16), if not specified shows all zones")

	// Short versions
	flag.StringVar(&windowFlag, "w", "", "Time window for aggregation (shorthand)")
	flag.StringVar(&rangeFlag, "r", "24h", "Time range to query (shorthand)")
	flag.StringVar(&controllerFlag, "c", "1", "Controller number (shorthand)")
	flag.StringVar(&zoneFlag, "z", "", "Zone number (shorthand)")

	flag.Parse()

	// If window not specified, use range value
	if windowFlag == "" {
		windowFlag = rangeFlag
	}

	// Validate time window and range
	if _, ok := timeWindows[windowFlag]; !ok {
		fmt.Printf("Invalid time window. Valid options are: %s\n", strings.Join(keys(timeWindows), ", "))
		os.Exit(1)
	}
	if _, ok := timeWindows[rangeFlag]; !ok {
		fmt.Printf("Invalid time range. Valid options are: %s\n", strings.Join(keys(timeWindows), ", "))
		os.Exit(1)
	}

	// Validate controller (always required, defaults to 1)
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
	query := buildQuery(influxDBBucket, timeWindows[rangeFlag], timeWindows[windowFlag], controllerFlag, zoneFlag)
	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		fmt.Printf("Query error: %s\n", err)
		os.Exit(1)
	}

	// Process results
	fmt.Printf("\nController: %s, Zone: %s\n", controllerFlag, zoneFlag)
	fmt.Printf("Time Window: %s, Range: %s\n\n", windowFlag, rangeFlag)

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

func keys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
