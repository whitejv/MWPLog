package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	// "sort" // Keep commented if not using sorted printout of initialized table

	"mwp_data_service/internal/aggregation" // Import the aggregation package
	"mwp_data_service/internal/config"      // Import the config package
	"mwp_data_service/internal/datastore"   // Import the datastore package
	"mwp_data_service/internal/service"     // Import the service package
)

// Constants for environment names
const (
	EnvProduction  = "production"
	EnvDevelopment = "development"
)

func main() {
	// Define command-line flags
	var configPath string
	var verbose bool
	var productionEnv bool
	var developmentEnv bool
	var queryTimeRange string // Added for time range flag

	// Setup flags
	flag.StringVar(&configPath, "config", "mwp_data_service/config/config.yaml", "Path to the configuration file")        // Adjusted default path
	flag.StringVar(&configPath, "c", "mwp_data_service/config/config.yaml", "Path to the configuration file (shorthand)") // Adjusted default path
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging (shorthand)")
	flag.BoolVar(&productionEnv, "P", false, "Use Production environment settings")
	flag.BoolVar(&developmentEnv, "D", false, "Use Development environment settings")
	flag.StringVar(&queryTimeRange, "range", "24h", "Time range for InfluxDB query (e.g., '24h', 'now', 'June')") // Added time range flag
	flag.StringVar(&queryTimeRange, "r", "24h", "Time range for InfluxDB query (shorthand)")                      // Added time range flag shorthand

	// Parse the flags
	flag.Parse()

	// Validate environment flags and determine environment
	selectedEnv, err := determineEnvironment(productionEnv, developmentEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error validating environment flags: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration from '%s': %v\n", configPath, err)
		os.Exit(1)
	}

	// Select the correct MQTT configuration based on the environment
	envCfg, ok := cfg.Environments[selectedEnv]
	if !ok {
		// This should theoretically not happen due to validation in LoadConfig, but good practice
		fmt.Fprintf(os.Stderr, "Error: Configuration for selected environment '%s' not found.\n", selectedEnv)
		os.Exit(1)
	}
	mqttCfg := envCfg.MQTT // Get the specific MQTT settings for the selected environment

	// Print status and loaded config (example)
	fmt.Printf("Starting MWP Data Service...\n")
	fmt.Printf("  Config file: %s\n", configPath)
	fmt.Printf("  Verbose mode: %t\n", verbose)
	fmt.Printf("  Environment: %s\n", selectedEnv)
	fmt.Printf("  Log Level: %s\n", cfg.Logging.Level)
	fmt.Printf("  InfluxDB Host: %s\n", cfg.InfluxDB.Host)
	fmt.Printf("  MQTT Broker: %s\n", mqttCfg.Broker)
	fmt.Printf("  MQTT Request Topic: %s\n", mqttCfg.RequestTopic)
	fmt.Printf("  MQTT Response Topic: %s\n", mqttCfg.ResponseTopic)

	// Initialize the WaterDataTable
	waterDataTable := datastore.InitializeTable(cfg.Controllers)
	fmt.Printf("Water data table initialized for %d controllers.\n", len(waterDataTable))
	// You can add a more detailed printout here if you want to inspect the structure
	// For example, to see how many zones each controller has:
	// var controllerIDs []string
	// for id := range waterDataTable {
	// 	controllerIDs = append(controllerIDs, id)
	// }
	// sort.Strings(controllerIDs) // To print in a consistent order
	// for _, id := range controllerIDs {
	// 	fmt.Printf("  Controller %s: %d zones initialized\n", id, len(waterDataTable[id]))
	// }

	// Write the initial water data table to JSON and text report files
	if err := service.WriteDataTableToJSON(waterDataTable, "output/watertable_initial.json"); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing initial data table to JSON file: %v\n", err)
	}
	if err := service.WriteDataTableAsTextReport(waterDataTable, "output/watertable_initial_report.txt"); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing initial data table to text report file: %v\n", err)
	}

	// --- Setup InfluxDB Client and attempt to query data ---
	fmt.Println("--- Attempting InfluxDB Operations ---")
	influxClient, err := aggregation.NewInfluxDBClient(cfg.InfluxDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating InfluxDB client: %v\n", err)
		// Decide if this is fatal. For now, we'll let it continue to see the stubbed query path.
	} else {
		defer influxClient.Close() // Ensure client is closed when main function exits
		fmt.Println("InfluxDB client created.")

		// Reset update status before processing new data
		datastore.ResetUpdateStatus(waterDataTable)
		fmt.Println("WaterDataTable update status reset.")

		// Use the time range from the command-line flag
		timeRange := queryTimeRange
		fmt.Printf("Attempting to query aggregated data for range: %s\n", timeRange)

		aggregatedRecords, err := aggregation.QueryAggregatedData(influxClient, timeRange, cfg.InfluxDB)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error querying aggregated data: %v\n", err)
		} else {
			fmt.Printf("Query returned %d records.\n", len(aggregatedRecords))
			if len(aggregatedRecords) > 0 {
				fmt.Println("Processing aggregated records...")
				for _, record := range aggregatedRecords {
					gpm := 0.0
					if record.TotalSeconds > 0 {
						gpm = record.TotalFlow / (record.TotalSeconds / 60.0)
					}
					datastore.UpdateEntry(waterDataTable, record.Controller, record.Zone,
						record.TotalFlow, record.TotalSeconds, record.AvgPSI,
						record.AvgTempF, record.AvgAmps, gpm)
				}
				fmt.Println("Finished processing aggregated records.")

				// Write the updated water data table to new files
				if err := service.WriteDataTableToJSON(waterDataTable, "output/watertable_after_query.json"); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing data table to JSON after query: %v\n", err)
				}
				if err := service.WriteDataTableAsTextReport(waterDataTable, "output/watertable_report_after_query.txt"); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing data table to text report after query: %v\n", err)
				}
			} else {
				fmt.Println("No records returned from query, watertable not updated further.")
			}
		}
	}

	fmt.Println("--- InfluxDB Operations Complete (or skipped due to client error) ---")
	fmt.Println("Service run complete (stub). Exiting.") // Changed from "Initialization complete"
	os.Exit(0)
}

// determineEnvironment validates the environment flags and returns the selected environment string
func determineEnvironment(prod, dev bool) (string, error) {
	if prod && dev {
		return "", errors.New("cannot specify both -P (Production) and -D (Development) flags")
	}
	if prod {
		return EnvProduction, nil
	}
	if dev {
		return EnvDevelopment, nil
	}
	// Default to Development if neither is specified
	fmt.Println("Warning: Neither -P nor -D specified, defaulting to Development environment.")
	return EnvDevelopment, nil
}
