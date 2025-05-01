# Project Plan: MWP Data Aggregation Service

## 1. Project Goal

The primary goal is to create a standalone Go application that runs continuously in the background on a Raspberry Pi. This service will act as a data aggregation engine for the Milano Water Pump (MWP) system data stored in InfluxDB. It will receive requests via MQTT specifying a time window, query InfluxDB for aggregated data across all relevant controllers and zones within that window, maintain a persistent internal state reflecting the latest data, and publish the complete state as a JSON payload via MQTT for consumption by a separate Blynk interface application.

This service replaces the functionality of the current `logit` command-line tool prototype, evolving it into a long-running service suitable for integration with a GUI frontend (Blynk).

## 2. System Architecture

The system consists of three main components:

1.  **Blynk GUI Application:** (External) The user interface where users select desired time windows (e.g., "Last 24h", "Last 7d") for viewing aggregated water system data.
2.  **MQTT Broker:** (External) A standard MQTT broker (e.g., Mosquitto) running locally or accessible on the network. It facilitates communication between the Go service and the Blynk interface application.
3.  **MWP Data Aggregation Service (This Go App):** The core component detailed in this plan. It connects to InfluxDB and the MQTT Broker, processes requests, aggregates data, maintains state, and publishes results.
4.  **InfluxDB:** (External) The time-series database storing the raw sensor data from the MWP system.
5.  **Blynk Interface Application:** (External) A separate application responsible for subscribing to the Go service's MQTT results topic, parsing the JSON data, formatting it appropriately for Blynk widgets, and handling communication with the Blynk platform (including sending the initial request to the Go service via MQTT).

**Communication Flow:**

```
+-----------+      MQTT Request       +---------------------+      InfluxDB Query       +-----------+
| Blynk GUI | ~~~~> (Request Topic) ~~~~> | Go Service (This App) | -------------------> | InfluxDB  |
|  (User)   |                           +---------------------+ <-------------------    +-----------+
+-----------+                                      |              InfluxDB Result
       ^                                           |
       |                                           | MQTT Response
       | Display Data                              | (Response Topic)
       |                                           v
+-----------+      Parse JSON         +-------------------------+
| Blynk App | <~~~~ (Response Topic) <~~~~ | Blynk Interface App |
| Widgets   |                           +-------------------------+
+-----------+                               (Separate Process)
```

## 3. Recommended Directory Layout (within `/home/pi/MWPLog`)

```
MWPLog/
├── bin/                  # <--- NEW: Installation target for all executables
├── build/                # <--- NEW: CMake build directory (out-of-source builds)
├── BlynkLog/             # C code for Blynk interface layer
│   └── CMakeLists.txt    # CMake build file for BlynkLog
├── CMakeLists.txt        # <--- NEW: Root CMake build file for the entire project
├── log_data/             # Go code for command-line tools
│   ├── cmd/
│   │   ├── logit/
│   │   └── logitdebug/
│   ├── docs/             # Documentation (CONTEXT.md, grafana_install.md)
│   └── CMakeLists.txt    # CMake build file for logit & logitdebug
├── mwp_data_service/     # Root for the Go background service
│   ├── cmd/
│   │   └── mwp_data_service/ # Main application entry point (main.go)
│   ├── config/             # Configuration files (e.g., config.yaml)
│   ├── internal/           # Internal packages for the service
│   │   ├── aggregation/    # Logic for querying InfluxDB & aggregating data
│   │   ├── datastore/      # Definition and management of the static data table
│   │   ├── mqtt/           # MQTT client setup, publishing, subscribing, callbacks
│   │   └── service/        # Core service logic, main loop, signal handling
│   ├── pkg/                # (Optional) Shared libraries if needed later
│   ├── go.mod              # Go module file
│   ├── go.sum
│   └── CMakeLists.txt    # CMake build file for mwp_data_service
├── mylib/                # NOTE: Source for mylib is external at /home/pi/MWPCore/myLib
├── notes/                # General project notes
│   └── mwp_data_service_plan.md # This document
├── SysLog/               # C code reference (Not built by CMake)
└── ...                   # Other existing files/directories
```
*Note: `mylib` source is external, but linked by `BlynkLog`. `SysLog` is excluded from the CMake build.* 

## 4. MWP Data Aggregation Service (Go App) Details

### 4.1. Core Functionality

*   Run as a standalone, persistent foreground application (managed by `cron`/shell scripts).
*   Connect to the specified MQTT Broker and InfluxDB instance upon startup.
*   Maintain a persistent, static internal data table representing the state of all defined Controller/Zone combinations.
*   Subscribe to a specific MQTT topic for incoming data requests.
*   Upon receiving a valid request:
    *   Reset the "updated" status of all entries in the internal table.
    *   Query InfluxDB based on the requested time window, aggregating data across all controllers/zones.
    *   Update the internal table with the newly aggregated metrics and mark updated entries.
    *   Publish the entire contents of the internal table as a JSON array to a specific MQTT response topic.
*   Handle OS signals (SIGINT, SIGTERM) for graceful shutdown (disconnect MQTT, close InfluxDB client).
*   Provide configurable logging.

### 4.2. Configuration (`config/config.yaml` - Example)

```yaml
influxdb:
  host: "http://192.168.1.250:8086"
  token: "YOUR_INFLUXDB_TOKEN_HERE" # Should be read securely
  org: "Milano"
  bucket: "MWPWater"

mqtt:
  broker: "tcp://localhost:1883" # Example: local broker
  client_id: "mwp_data_aggregator_service"
  request_topic: "mwp/data/query_request"
  response_topic: "mwp/data/query_results"
  # Add username/password if required by broker

logging:
  level: "info" # e.g., debug, info, warn, error
  file: "/var/log/mwp_data_service.log" # Optional: log to file

controllers:
  default_zones: 5
  special_controllers:
    "1": 32
    "2": 32
  zone_start_index: 1 # Assume zones are 1-based

```

*   Configuration should be loaded at startup. Using a library like Viper is recommended.

### 4.3. Internal Data Structure (`internal/datastore/store.go`)

*   **`ZoneData` Struct:**
    ```go
    package datastore

    type ZoneData struct {
        TotalFlow          float64 `json:"totalFlow"`
        TotalSeconds       float64 `json:"totalSeconds"`
        AvgPSI             float64 `json:"avgPSI"`
        AvgTempF           float64 `json:"avgTempF"`
        AvgAmps            float64 `json:"avgAmps"`
        UpdatedInLastQuery bool    `json:"updatedInLastQuery"`
    }
    ```
*   **`WaterDataTable` Type:**
    ```go
    package datastore

    // Map key: Controller ID (string), Inner Map key: Zone ID (string)
    type WaterDataTable map[string]map[string]ZoneData
    ```
*   **Initialization:** A function `InitializeTable(config)` will create and populate the `WaterDataTable` based on the configuration (controllers 0-5, zone counts 5 or 32, zone start index). All `ZoneData` entries will be initialized with zeros and `UpdatedInLastQuery: false`.
*   **Update Logic:** Functions `ResetUpdateStatus(table WaterDataTable)` and `UpdateEntry(table WaterDataTable, controller, zone string, metrics AggregatedMetrics)` will handle modifying the table based on query results.

### 4.4. MQTT Handling (`internal/mqtt/client.go`)

*   Uses `paho.mqtt.golang` library.
*   Connects to the broker specified in the config.
*   **Subscription:** Subscribes to the `request_topic` upon successful connection.
*   **Message Callback (`onMessageReceived`):**
    *   Attached to the client, triggered by messages on the `request_topic`.
    *   Parses the incoming message payload (expected format TBD, likely simple string like "24h", "7d", or JSON `{"range": "24h"}`).
    *   Validates the requested time range against allowed values.
    *   If valid, triggers the data aggregation process (potentially via a channel).
    *   Logs errors if parsing/validation fails.
*   **Publishing:** A function `PublishResults(client MQTTClient, topic string, dataTable datastore.WaterDataTable)` will:
    *   Convert the `dataTable` into the required JSON array format (iterating through all C/Z, creating objects with C, Z, and ZoneData fields).
    *   Publish the JSON string to the `response_topic`.
*   **Connection Handling:** Implements `onConnect` and `onConnectionLost` callbacks for logging and potential reconnection logic (though `cron` restart might suffice).

### 4.5. InfluxDB Aggregation (`internal/aggregation/aggregator.go`)

*   Uses `influxdb-client-go/v2` library.
*   **`QueryAggregatedData` Function:**
    *   Takes the InfluxDB client and the requested time range string (e.g., "-24h") as input.
    *   Constructs the Flux query (similar to the final Grafana prototype query) to:
        *   Filter by the time range.
        *   Filter by measurement `mwp_sensors`.
        *   Group by `Controller` and `Zone`.
        *   Calculate `sum()` for `intervalFlow`, `secondsOn`.
        *   Calculate `mean()` for `pressurePSI`, `temperatureF`, `amperage`.
        *   Join/Pivot the results into a stream where each row represents one Controller/Zone with columns for all aggregated metrics.
    *   Executes the query.
    *   Parses the query results into a temporary structure (e.g., a slice of structs or maps) containing Controller, Zone, and the calculated metrics. Returns this structure.
    *   Handles query errors.

### 4.6. Core Service Logic (`internal/service/service.go` and `cmd/mwp_data_service/main.go`)

*   **`main.go`:**
    *   Parses command-line flags (e.g., `-config <path>`).
    *   Loads configuration.
    *   Initializes logging.
    *   Initializes the static `WaterDataTable`.
    *   Initializes and connects the InfluxDB client.
    *   Initializes and connects the MQTT client (setting up callbacks).
    *   Sets up OS signal handling (`signal.Notify`) for graceful shutdown.
    *   Enters the main service loop (which might just be waiting for signals or MQTT activity).
*   **`service.go`:**
    *   Contains the main loop/coordination logic.
    *   Orchestrates the response to an incoming MQTT request:
        1.  Call `datastore.ResetUpdateStatus`.
        2.  Call `aggregation.QueryAggregatedData`.
        3.  Loop through query results, calling `datastore.UpdateEntry`.
        4.  Call `mqtt.PublishResults`.
    *   Handles the graceful shutdown sequence upon receiving a signal.

## 5. Management and Operation

*   **Building:** The entire project (Go service, C components, Go tools) is built using CMake.
    1.  **Configure (run once, or after changing CMakeLists.txt):**
        ```bash
        # Navigate to the project root
        cd /home/pi/MWPLog
        # Create a build directory (if it doesn't exist)
        mkdir -p build
        cd build
        # Configure the build system (e.g., generate Makefiles)
        cmake ..
        ```
    2.  **Compile:**
        ```bash
        # Still inside the build directory
        make
        ```
        This compiles `blynkLog` and runs the `go build` commands defined in the CMake files for `mwp_data_service`, `logit`, and `logitdebug`. Build artifacts are placed within the `build` directory.

*   **Installation:** The compiled executables are installed into the project-local `bin` directory.
    ```bash
    # Still inside the build directory
    # Ensure /home/pi/MWPLog/bin exists first
    make install
    ```
    This copies `blynkLog`, `mwp_data_service`, `logit`, `logitdebug` to `/home/pi/MWPLog/bin`.

*   **Running `mwp_data_service`:** Executed via a shell script, typically pointing to the executable in the `bin` directory.
    ```bash
    # Example: Start script (start_mwp_service.sh)
    #!/bin/bash
    PROJECT_DIR="/home/pi/MWPLog"
    CONFIG_FILE="${PROJECT_DIR}/mwp_data_service/config/config.yaml"
    APP_PATH="${PROJECT_DIR}/bin/mwp_data_service"
    LOG_FILE="/var/log/mwp_data_service.log" # Or another location

    # Basic check to prevent multiple instances (using pidof)
    if pidof -o %PPID -x "$0" > /dev/null; then
        echo "Script is already running."
        exit 1
    fi
    if pidof -x "$(basename "$APP_PATH")" > /dev/null; then
        echo "Application is already running."
        # Optional: kill existing process if desired?
        exit 1
    fi

    echo "Starting MWP Data Service from ${APP_PATH}..."
    # Run in background, redirect output
    nohup "$APP_PATH" -config "$CONFIG_FILE" >> "$LOG_FILE" 2>&1 &
    echo "Service started."
    exit 0
    ```
*   **Stopping:** A corresponding `stop_mwp_service.sh` script would find the PID of `mwp_data_service` and send it a `SIGTERM` signal using `kill <PID>`.
*   **Cron:** `cron` can be used to schedule the `start_mwp_service.sh` and `stop_mwp_service.sh` scripts.

## 6. Next Steps / Open Questions

*   Finalize the exact request message format (simple string vs. JSON).
*   Confirm zone numbering scheme (1-5 default, 1-32 for C1/C2?).
*   Implement configuration loading (Viper).
*   Implement logging (Logrus or standard log).
*   Implement signal handling.
*   Develop MQTT connection/callback logic.
*   Develop InfluxDB query execution/parsing.
*   Develop data table initialization and update logic.
*   Develop JSON marshalling for the response.
*   Write unit/integration tests where applicable.
*   Create start/stop shell scripts.
*   Configure `cron` jobs.
*   Verify/Create `CMakeLists.txt` for external `/home/pi/MWPCore/myLib` if `BlynkLog` build fails link stage.

## 7. Status of Existing `log_data` Tools (`logit`, `logitdebug`)

It is explicitly intended that the existing Go command-line tools located in the `log_data/` directory (`logit` and `logitdebug`) will **remain separate and functional**.

*   **Purpose:** They will continue to serve as standalone utilities for direct command-line querying of InfluxDB data (aggregated via `logit`, raw via `logitdebug`).
*   **Use Cases:** Useful for quick checks, debugging data issues directly, and comparing results with the new background service during development and operation.
*   **Build Process:** These tools are now built as part of the unified CMake build process defined in `log_data/CMakeLists.txt` and the root `CMakeLists.txt`.
*   **Installation:** They are installed alongside the other project executables into `/home/pi/MWPLog/bin` via `make install`.
*   **Maintenance Note:** If future changes are made to the InfluxDB schema or core aggregation logic within the `mwp_data_service`, corresponding manual updates may be required for `logit` and `logitdebug` if they need to reflect those changes. 