package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"

	// "sort" // Keep commented if not using sorted printout of initialized table

	"mwp_data_service/internal/aggregation" // Import the aggregation package
	"mwp_data_service/internal/config"      // Import the config package
	"mwp_data_service/internal/datastore"   // Import the datastore package
	"mwp_data_service/internal/service"     // Import the service package
)

// Constants for environment names
const (
	EnvProduction         = "production"
	EnvDevelopment        = "development"
	DefaultRangeAfterNow  = "1h" // Default query range after "now" mode times out
	NowModeDuration       = 60 * time.Second
	NowModeUpdateInterval = 1 * time.Second
)

// Global variables (consider encapsulating in a struct for better organization later)
var (
	waterDataTable datastore.WaterDataTable
	influxClient   influxdb2.Client // Corrected type
	mqttClient     mqtt.Client
	appConfig      *config.Config
	mqttConfig     config.MQTTConfig
	verboseMode    bool

	// Mutex to protect WaterDataTable and associated summaries during concurrent access
	dataMutex = &sync.Mutex{}

	// "Now" mode variables
	isInNowMode         bool
	nowModeTicker       *time.Ticker
	nowModeTimeoutTimer *time.Timer
	lastNonNowRange     string        = DefaultRangeAfterNow // Store the last explicitly requested range or default
	nowModeControlChan  chan string                          // Used to signal changes to "now" mode (e.g., stop, new_range)
	shutdownChan        chan struct{}                        // Used to signal goroutines to terminate
)

// Define a struct for MQTT request payload
type RangeRequestPayload struct {
	Range string `json:"range"`
}

// processAndPublishData encapsulates the logic to query data, update table, and publish.
// It's called initially and then by the MQTT message handler.
func processAndPublishData(timeRange string) {
	dataMutex.Lock()
	defer dataMutex.Unlock()

	if influxClient == nil {
		fmt.Println("InfluxDB client is not initialized. Skipping data processing.")
		return
	}

	fmt.Printf("Attempting to query aggregated data for range: %s\n", timeRange)
	aggregatedRecords, err := aggregation.QueryAggregatedData(influxClient, timeRange, appConfig.InfluxDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying aggregated data for range '%s': %v\n", timeRange, err)
		return // Or publish an error state via MQTT?
	}

	fmt.Printf("Query for range '%s' returned %d records.\n", timeRange, len(aggregatedRecords))

	// Reset update status before processing new data
	datastore.ResetUpdateStatus(waterDataTable)
	if verboseMode {
		fmt.Println("WaterDataTable update status reset.")
	}

	var totalIrrigationGallons float64
	var totalWell3Gallons float64

	if len(aggregatedRecords) > 0 {
		if verboseMode {
			fmt.Println("Processing aggregated records...")
		}
		for _, record := range aggregatedRecords {
			if record.Controller == "0" || record.Controller == "1" || record.Controller == "2" {
				totalIrrigationGallons += record.TotalFlow
			}
			if record.Controller == "3" && record.Zone == "1" {
				totalWell3Gallons += record.TotalFlow
			}
			gpm := 0.0
			if record.TotalSeconds > 0 {
				gpm = record.TotalFlow / (record.TotalSeconds / 60.0)
			}
			datastore.UpdateEntry(waterDataTable, record.Controller, record.Zone,
				record.TotalFlow, record.TotalSeconds, record.AvgPSI,
				record.AvgTempF, record.AvgAmps, gpm)
		}
		if verboseMode {
			fmt.Println("Finished processing aggregated records.")
		}
	} else {
		fmt.Printf("No records returned from query for range '%s', watertable not updated further for this cycle.\n", timeRange)
	}

	if verboseMode {
		fmt.Printf("Super Summary - Total Irrigation Gallons (C0,C1,C2): %.2f\n", totalIrrigationGallons)
		fmt.Printf("Super Summary - Total Well 3 Gallons (C3Z1): %.2f\n", totalWell3Gallons)
	}

	// Publish to MQTT
	reportData := service.ReportData{
		TotalIrrigationGallons: totalIrrigationGallons,
		TotalWell3Gallons:      totalWell3Gallons,
		Details:                waterDataTable,
	}
	jsonData, err := json.Marshal(reportData) // Using standard Marshal, Indent is for file_writer's own use
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshalling report data to JSON: %v\n", err)
		return
	}

	if mqttClient != nil && mqttClient.IsConnected() {
		token := mqttClient.Publish(mqttConfig.ResponseTopic, 1, false, jsonData) // QoS 1
		// Wait for a small amount of time for the publish to complete, but don't block indefinitely.
		if token.WaitTimeout(2*time.Second) && token.Error() != nil {
			fmt.Fprintf(os.Stderr, "Error publishing data to MQTT topic %s: %v\n", mqttConfig.ResponseTopic, token.Error())
		} else if verboseMode {
			if token.Error() == nil { // Check error again after WaitTimeout
				fmt.Printf("Published data to MQTT topic: %s\n", mqttConfig.ResponseTopic)
			} else {
				// This case might occur if WaitTimeout returns false but token.Error() was already set.
				// Or if an error occurred but WaitTimeout also timed out.
				fmt.Fprintf(os.Stderr, "Failed to confirm MQTT publish to %s (timeout or error): %v\n", mqttConfig.ResponseTopic, token.Error())
			}
		}
	} else {
		fmt.Println("MQTT client not connected. Cannot publish data.")
	}

	// Optionally, still write to local files for debugging/backup
	if err := service.WriteDataTableToJSON(waterDataTable, totalIrrigationGallons, totalWell3Gallons, "output/watertable_latest.json"); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing latest data table to JSON: %v\n", err)
	}
	if err := service.WriteDataTableAsTextReport(waterDataTable, totalIrrigationGallons, totalWell3Gallons, "output/watertable_latest_report.txt", timeRange); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing latest data table to text report: %v\n", err)
	}
}

// onMQTTConnect is called when the MQTT client successfully connects.
var onMQTTConnect mqtt.OnConnectHandler = func(client mqtt.Client) {
	fmt.Println("MQTT Connected.")
	// Subscribe to the range request topic
	if token := client.Subscribe(mqttConfig.RequestTopic, 1, mqttMessageHandler); token.Wait() && token.Error() != nil {
		fmt.Fprintf(os.Stderr, "Error subscribing to MQTT topic '%s': %v\n", mqttConfig.RequestTopic, token.Error())
		// Consider a retry mechanism or exiting if subscription is critical
	} else {
		fmt.Printf("Subscribed to MQTT topic: %s\n", mqttConfig.RequestTopic)
	}
}

// onMQTTConnectionLost is called when the MQTT client loses its connection.
var onMQTTConnectionLost mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	fmt.Printf("MQTT Connection lost: %v. Will attempt to reconnect.\n", err)
	// Reconnect logic is often handled by Paho's auto-reconnect if enabled in options.
}

// mqttMessageHandler is called when a message arrives on a subscribed topic.
var mqttMessageHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	if verboseMode {
		fmt.Printf("Received MQTT message on topic: %s\n", msg.Topic())
		fmt.Printf("Payload: %s\n", string(msg.Payload()))
	}

	var payload RangeRequestPayload
	err := json.Unmarshal(msg.Payload(), &payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error unmarshalling MQTT payload: %v\n", err)
		return
	}

	if payload.Range == "" {
		fmt.Fprintf(os.Stderr, "Received empty range in MQTT payload.\n")
		return
	}

	// Signal the nowModeManager goroutine about the new request.
	// This centralizes control over nowMode state transitions.
	nowModeControlChan <- payload.Range
}

// stopNowMode stops the ticker and timer associated with "now" mode.
// It ensures that it does not operate on nil pointers.
func stopNowModeActiveProcesses() {
	dataMutex.Lock() // Protect access to now mode global vars
	defer dataMutex.Unlock()

	if nowModeTicker != nil {
		nowModeTicker.Stop()
		nowModeTicker = nil // Important to allow GC and prevent reuse of stopped ticker
		if verboseMode {
			fmt.Println("Now mode ticker stopped.")
		}
	}
	if nowModeTimeoutTimer != nil {
		// Stop the timer and drain its channel if necessary, though for time.Timer, Stop() is usually enough.
		if !nowModeTimeoutTimer.Stop() {
			// Timer already fired or was stopped, attempt to drain if not already done.
			// This is a bit tricky; usually, Stop() handles it. This is a safeguard.
			select {
			case <-nowModeTimeoutTimer.C:
			default:
			}
		}
		nowModeTimeoutTimer = nil
		if verboseMode {
			fmt.Println("Now mode timeout timer stopped.")
		}
	}
	isInNowMode = false
}

// nowModeManager is a goroutine that manages the "now" mode lifecycle.
func nowModeManager() {
	defer fmt.Println("nowModeManager goroutine finished.")

loop:
	for {
		select {
		case newRangeRequest := <-nowModeControlChan:
			dataMutex.Lock() // Lock for modifying nowMode state variables
			if verboseMode {
				fmt.Printf("nowModeManager: Received control signal for range: %s\n", newRangeRequest)
			}

			// Stop any existing "now" mode operations before starting new or switching mode
			if isInNowMode {
				// No need to call stopNowModeActiveProcesses here, as we are already in dataMutex lock
				if nowModeTicker != nil {
					nowModeTicker.Stop()
					nowModeTicker = nil
				}
				if nowModeTimeoutTimer != nil {
					if !nowModeTimeoutTimer.Stop() {
						select {
						case <-nowModeTimeoutTimer.C:
						default:
						}
					}
					nowModeTimeoutTimer = nil
				}
				isInNowMode = false // Explicitly mark as not in now mode before potentially re-entering
			}

			if newRangeRequest == "now" {
				fmt.Println("Starting 'now' mode.")
				isInNowMode = true
				// Ensure previous ticker/timer are fully stopped and nilled before creating new ones.
				// Redundant if logic above is sound, but safe.
				if nowModeTicker != nil {
					nowModeTicker.Stop()
					nowModeTicker = nil
				}
				if nowModeTimeoutTimer != nil {
					nowModeTimeoutTimer.Stop()
					nowModeTimeoutTimer = nil
				}

				nowModeTicker = time.NewTicker(NowModeUpdateInterval)
				nowModeTimeoutTimer = time.NewTimer(NowModeDuration)
				dataMutex.Unlock() // Unlock before potentially long-running processAndPublishData

				processAndPublishData("-1m") // Initial data publish for "now" mode
			} else {
				fmt.Printf("Received specific range request: %s. Stopping 'now' mode if active.\n", newRangeRequest)
				lastNonNowRange = newRangeRequest // Update the last non-"now" range
				isInNowMode = false               // Ensure now mode is marked as off
				dataMutex.Unlock()                // Unlock before potentially long-running processAndPublishData

				processAndPublishData(newRangeRequest)
			}

		case <-func() <-chan time.Time { // Anonymous func to safely access potentially nil ticker
			dataMutex.Lock()
			defer dataMutex.Unlock()
			if nowModeTicker == nil {
				return nil // Return a nil channel if ticker is not active, blocking this case
			}
			return nowModeTicker.C
		}():
			if verboseMode {
				fmt.Println("'Now' mode tick: processing -1m data.")
			}
			// isInNowMode check is implicitly handled by ticker being non-nil
			processAndPublishData("-1m") // Process data for the tick

		case <-func() <-chan time.Time { // Anonymous func to safely access potentially nil timer
			dataMutex.Lock()
			defer dataMutex.Unlock()
			if nowModeTimeoutTimer == nil {
				return nil // Return a nil channel if timer is not active
			}
			return nowModeTimeoutTimer.C
		}():
			dataMutex.Lock()
			if verboseMode {
				fmt.Println("'Now' mode timed out. Reverting to default range.")
			}
			if nowModeTicker != nil { // Stop the ticker
				nowModeTicker.Stop()
				nowModeTicker = nil
			}
			nowModeTimeoutTimer = nil // Timer has fired, so it's done. Nil it out.
			isInNowMode = false
			currentDefaultRange := lastNonNowRange // Use the stored last non-"now" range
			dataMutex.Unlock()

			fmt.Printf("Now mode finished. Processing data for default range: %s\n", currentDefaultRange)
			processAndPublishData(currentDefaultRange)

		case <-shutdownChan:
			fmt.Println("nowModeManager: Received shutdown signal.")
			stopNowModeActiveProcesses() // Clean up any active ticker/timer
			dataMutex.Lock()             // Lock to ensure isInNowMode is set correctly before exit
			isInNowMode = false
			dataMutex.Unlock()
			break loop // Exit the for loop
		}
	}
}

func main() {
	var configPath string
	var initialQueryRange string

	// Initialize channels
	nowModeControlChan = make(chan string, 1) // Buffer of 1 to prevent blocking sender if manager is busy
	shutdownChan = make(chan struct{})

	flag.StringVar(&configPath, "config", "mwp_data_service/config/config.yaml", "Path to the configuration file")
	flag.StringVar(&configPath, "c", "mwp_data_service/config/config.yaml", "Path to the configuration file (shorthand)")
	flag.BoolVar(&verboseMode, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&verboseMode, "v", false, "Enable verbose logging (shorthand)")
	var productionEnv bool
	flag.BoolVar(&productionEnv, "P", false, "Use Production environment settings")
	var developmentEnv bool
	flag.BoolVar(&developmentEnv, "D", false, "Use Development environment settings")
	flag.StringVar(&initialQueryRange, "range", "24h", "Initial time range for InfluxDB query (e.g., '24h', 'June', '2023'). 'now' is not allowed on startup.")
	flag.StringVar(&initialQueryRange, "r", "24h", "Initial time range for InfluxDB query (shorthand)")
	flag.Parse()

	if initialQueryRange == "now" {
		fmt.Fprintf(os.Stderr, "Error: -range 'now' is not permitted on service startup. Please use a specific duration, month, or year.\n")
		flag.Usage()
		os.Exit(1)
	}

	selectedEnv, err := determineEnvironment(productionEnv, developmentEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error validating environment flags: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	var cfgErr error
	appConfig, cfgErr = config.LoadConfig(configPath)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration from '%s': %v\n", configPath, cfgErr)
		os.Exit(1)
	}

	envCfg, ok := appConfig.Environments[selectedEnv]
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: Configuration for selected environment '%s' not found.\n", selectedEnv)
		os.Exit(1)
	}
	mqttConfig = envCfg.MQTT // Assign to global mqttConfig

	fmt.Printf("Starting MWP Data Service...\n")
	fmt.Printf("  Config file: %s\n", configPath)
	fmt.Printf("  Verbose mode: %t\n", verboseMode)
	fmt.Printf("  Environment: %s\n", selectedEnv)
	fmt.Printf("  Log Level: %s\n", appConfig.Logging.Level)
	fmt.Printf("  InfluxDB Host: %s\n", appConfig.InfluxDB.Host)
	fmt.Printf("  MQTT Broker: %s\n", mqttConfig.Broker)
	fmt.Printf("  MQTT Request Topic: %s\n", mqttConfig.RequestTopic)
	fmt.Printf("  MQTT Response Topic: %s\n", mqttConfig.ResponseTopic)

	// Initialize WaterDataTable (global)
	dataMutex.Lock()
	waterDataTable = datastore.InitializeTable(appConfig.Controllers)
	lastNonNowRange = initialQueryRange // Initialize with the startup range
	dataMutex.Unlock()
	fmt.Printf("Water data table initialized for %d controllers.\n", len(waterDataTable))

	// Write initial state files (using 0 for summaries as they are not yet calculated from live data)
	if err := service.WriteDataTableToJSON(waterDataTable, 0.0, 0.0, "output/watertable_initial.json"); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing initial data table to JSON file: %v\n", err)
	}
	if err := service.WriteDataTableAsTextReport(waterDataTable, 0.0, 0.0, "output/watertable_initial_report.txt", initialQueryRange); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing initial data table to text report file: %v\n", err)
	}

	// Initialize InfluxDB Client (global)
	var influxErr error
	influxClient, influxErr = aggregation.NewInfluxDBClient(appConfig.InfluxDB)
	if influxErr != nil {
		fmt.Fprintf(os.Stderr, "Error creating InfluxDB client: %v. Service will run without InfluxDB functionality initially.\n", influxErr)
		// Allow service to continue, MQTT might still work for commands, but data processing will fail until client is up.
	} else {
		// Perform initial data load and publish
		fmt.Println("--- Performing Initial Data Load ---")
		processAndPublishData(initialQueryRange)
		fmt.Println("--- Initial Data Load Complete ---")
	}

	// Start the nowModeManager goroutine
	go nowModeManager()

	// Setup MQTT Client
	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttConfig.Broker)
	opts.SetClientID(mqttConfig.ClientID)
	// opts.SetUsername(mqttConfig.Username) // Uncomment if auth is needed
	// opts.SetPassword(mqttConfig.Password) // Uncomment if auth is needed
	opts.SetDefaultPublishHandler(mqttMessageHandler) // Default handler for unhandled topics (optional)
	opts.OnConnect = onMQTTConnect
	opts.OnConnectionLost = onMQTTConnectionLost
	opts.SetAutoReconnect(true) // Enable auto-reconnect
	opts.SetMaxReconnectInterval(1 * time.Minute)

	mqttClient = mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		// This is a critical failure if MQTT is essential.
		// For robustness, could retry connection here or let auto-reconnect handle it.
		fmt.Fprintf(os.Stderr, "Error connecting to MQTT broker: %v. Service will run, but MQTT functionality will be impaired until connection succeeds.\n", token.Error())
	}

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("Service is running. Press Ctrl+C to exit.")
	<-sigChan // Block until a signal is received

	// Graceful shutdown
	fmt.Println("Shutting down service...")

	// Signal goroutines to shut down
	close(shutdownChan) // Close channel to signal all listeners

	if mqttClient != nil && mqttClient.IsConnected() {
		// Unsubscribe if needed, though disconnecting should handle it.
		// client.Unsubscribe(mqttConfig.RequestTopic)
		mqttClient.Disconnect(250) // 250ms timeout
		fmt.Println("MQTT client disconnected.")
	}
	if influxClient != nil {
		// Assuming InfluxDB client has a Close method.
		// If it's the raw client from influxdb-client-go/v2, it's influxdb2.Client
		// The type of global influxClient might need to be influxdb2.Client
		// For now, let's assume a Close() method exists as per previous defer.
		influxClient.Close() // Explicitly close the InfluxDB client
		fmt.Println("InfluxDB client closed.")
	}
	fmt.Println("Service exited.")
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
