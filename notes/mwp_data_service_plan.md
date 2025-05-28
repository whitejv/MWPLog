# Project Plan: MWP Data Aggregation Service

## 1. Project Goal

The primary goal is to create a standalone Go application that runs continuously in the background on a Raspberry Pi. This service will act as a data aggregation engine for the Milano Water Pump (MWP) system data stored in InfluxDB. It will receive requests via MQTT specifying a time window, query InfluxDB for aggregated data across all relevant controllers and zones within that window, maintain a persistent internal state reflecting the latest data, and publish the complete state as a JSON payload via MQTT for consumption by a separate Blynk interface application.

This service replaces the functionality of the current `logit` command-line tool prototype, evolving it into a long-running service suitable for integration with a GUI frontend (Blynk).

## 2. System Architecture

The system consists of several key components:

1.  **Blynk GUI Application:** (External) The user interface on a phone/tablet where users interact with Blynk widgets, including selecting the desired time window (e.g., "Last 24h", "Last 7d") for viewing aggregated water system data.
2.  **Blynk Platform:** (External Cloud Service) Handles communication between the Blynk GUI App and the device-side interface application.
3.  **MQTT Broker:** (External, likely local) A standard MQTT broker (e.g., Mosquitto) running locally or accessible on the network. It facilitates communication between the Go service and the C Blynk interface application.
4.  **Blynk Interface Application (`BlynkLog`):** (Existing C App) This application connects *both* to the Blynk Platform (using Blynk protocols/MQTT) and the local MQTT Broker.
    *   It receives user input/commands (like time window selection) *from* the Blynk Platform.
    *   It translates these commands into standardized MQTT request messages.
    *   It publishes these request messages to the Go service's request topic on the local MQTT Broker.
    *   It subscribes to the Go service's response topic on the local MQTT Broker.
    *   It receives the JSON results from the Go service.
    *   It parses the JSON and updates the appropriate Blynk widgets via the Blynk Platform.
5.  **MWP Data Aggregation Service (This Go App):** The core component detailed in this plan. It connects *only* to the local MQTT Broker and InfluxDB.
    *   Subscribes to the request topic on the local MQTT Broker.
    *   Processes requests received from the `BlynkLog` app.
    *   Aggregates data from InfluxDB.
    *   Maintains the static internal state.
    *   Publishes results (JSON) to the response topic on the local MQTT Broker.
6.  **InfluxDB:** (External) The time-series database storing the raw sensor data from the MWP system.

**Communication Flow:**

```
+-----------+      Blynk       +-----------------+      MQTT Request       +---------------------+      InfluxDB       +-----------+
| Blynk GUI | <~~> Platform <~~> | BlynkLog C App  | ~~~~> (Request Topic) ~~~~> | Go Service (This App) | ~~~> Query ~~~> | InfluxDB  |
|  (User)   |                   +-----------------+                           +---------------------+ <~~ Result ~~~    +-----------+
+-----------+                          |         Local MQTT Broker           |        (Local)
       ^                               |                                     |
       | Update Widgets                | MQTT Response                       |
       | (via Blynk Platform)          | (Response Topic)                    |
       +-------------------------------|-------------------------------------+
                                       v
                                Parse JSON &
                                Update Blynk
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
│   │   └── service/        # Core service logic, main loop, signal handling, file output
│   ├── pkg/                # (Optional) Shared libraries if needed later
│   ├── go.mod              # Go module file
│   ├── go.sum
│   └── CMakeLists.txt    # CMake build file for mwp_data_service
├── mylib/                # NOTE: Source for mylib is external at /home/pi/MWPCore/myLib
├── notes/                # General project notes
│   └── mwp_data_service_plan.md # This document
├── output/               # <--- NEW: Directory for generated output files (e.g., JSON, text reports)
├── SysLog/               # C code reference (Not built by CMake)
└── ...                   # Other existing files/directories
```
*Note: `mylib` source is external, but linked by `BlynkLog`. `SysLog` is excluded from the CMake build. The `output/` directory is created by the `mwp_data_service` if it doesn't exist.*

## 4. MWP Data Aggregation Service (Go App) Details

### 4.1. Core Functionality (Original Plan)

*   Run as a standalone, persistent foreground application (managed by `cron`/shell scripts).
*   Connect to the specified MQTT Broker and InfluxDB instance upon startup.
*   Maintain a persistent, static internal data table representing the state of all defined Controller/Zone combinations.
*   Subscribe to a specific MQTT topic for incoming data requests.
*   Upon receiving a valid request:
    *   Reset the "updated" status of all entries in the internal table.
    *   Query InfluxDB based on the requested time window, aggregating data across all controllers/zones.
    *   Update the internal table with the newly aggregated metrics (calculating GPM) and mark updated entries.
    *   Publish the entire contents of the internal table as a JSON array to a specific MQTT response topic.
*   Handle OS signals (SIGINT, SIGTERM) for graceful shutdown (disconnect MQTT, close InfluxDB client).
*   Provide configurable logging.

### 4.2. Configuration (`mwp_data_service/config/config.yaml`)

*   The configuration file defines InfluxDB connection details, logging preferences, controller/zone structures (including `total_controllers`), and environment-specific (development/production) MQTT settings.
*   It is loaded at startup by `internal/config/config.go`.

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
        GPM                float64 `json:"gpm"`                // Calculated by the service
        UpdatedInLastQuery bool    `json:"updatedInLastQuery"`
    }
    ```
*   **`WaterDataTable` Type:**
    ```go
    package datastore

    // Map key: Controller ID (string), Inner Map key: Zone ID (string)
    type WaterDataTable map[string]map[string]ZoneData
    ```
*   **Initialization:** `InitializeTable(cfg config.ControllerConfig)` creates and populates the `WaterDataTable` based on `config.yaml`.
*   **Update Logic:** `ResetUpdateStatus(table WaterDataTable)` and `UpdateEntry(table WaterDataTable, ...)` functions are available.

### 4.4. MQTT Handling (`internal/mqtt/client.go`)

*   *(Planned, not yet implemented)*
*   Will use `paho.mqtt.golang` library.
*   Connect to the broker specified in the config.
*   Subscribe to the `request_topic`.
*   Handle incoming messages to trigger data aggregation.
*   Publish results (JSON `WaterDataTable`) to the `response_topic`.

### 4.5. InfluxDB Aggregation (`internal/aggregation/aggregator.go`)

*   Uses `influxdb-client-go/v2` library.
*   `NewInfluxDBClient(cfg config.InfluxDBConfig)` function initializes the InfluxDB client.
*   `AggregatedRecord` struct defined to hold raw query results before GPM calculation.
*   `QueryAggregatedData(client influxdb2.Client, timeRange string, dbConfig config.InfluxDBConfig)`:
    *   Constructs and executes a Flux query to fetch `sum(intervalFlow)`, `sum(secondsOn)`, `mean(pressurePSI)`, `mean(temperatureF)`, `mean(amperage)` grouped by Controller and Zone.
    *   Filters out records with missing/empty Controller or Zone tags.
    *   Pivots results and maps them to `AggregatedRecord` structs.
    *   Handles query errors and result parsing.

### 4.6. Core Service Logic (`internal/service/service.go` and `cmd/mwp_data_service/main.go`)

*   **`cmd/mwp_data_service/main.go`:**
    *   Parses command-line flags (`-config`, `-P`, `-D`, `-verbose`).
    *   Loads configuration from `config.yaml`.
    *   Initializes the static `WaterDataTable`.
    *   Creates an InfluxDB client.
    *   **Currently:** Executes a one-off data aggregation:
        1.  Calls `datastore.ResetUpdateStatus`.
        2.  Calls `aggregation.QueryAggregatedData` with a hardcoded time range (e.g., "-24h").
        3.  Loops through query results, calculates GPM (`TotalFlow / (TotalSeconds / 60.0)`), and calls `datastore.UpdateEntry`.
        4.  Writes the `WaterDataTable` to `output/watertable_initial.json`, `output/watertable_initial_report.txt` (before query) and `output/watertable_after_query.json`, `output/watertable_report_after_query.txt` (after query attempt). The text report displays "TotalMinutes".
    *   Exits after the one-off execution.
*   **`internal/service/file_writer.go`:**
    *   `WriteDataTableToJSON`: Writes the `WaterDataTable` to a JSON file.
    *   `WriteDataTableAsTextReport`: Writes the `WaterDataTable` to a human-readable, formatted text file (displays TotalMinutes).
*   *(Planned for `internal/service/service.go`): Main service loop, orchestration of MQTT request to InfluxDB query and response, signal handling.*

## 5. Development Progress & Current Status (as of 2024-07-26)

The `mwp_data_service` application has been developed to a point where it can perform a one-time data aggregation from InfluxDB and output the results to files.

**Implemented Features:**

*   **Project Structure:** The Go module `mwp_data_service` is set up with the planned internal package structure (`cmd`, `config`, `datastore`, `aggregation`, `service`).
*   **Configuration:**
    *   `config.yaml` defines InfluxDB, MQTT (for dev/prod), logging, and controller/zone parameters (including `total_controllers`).
    *   `internal/config/config.go` loads and parses this YAML.
*   **Data Storage (`internal/datastore`):**
    *   `ZoneData` struct includes fields for `TotalFlow`, `TotalSeconds`, `AvgPSI`, `AvgTempF`, `AvgAmps`, and a service-calculated `GPM`.
    *   `WaterDataTable` (map-based) stores `ZoneData` for all configured controllers and zones.
    *   `InitializeTable` correctly populates the table based on configuration.
    *   `ResetUpdateStatus` and `UpdateEntry` functions are implemented.
*   **InfluxDB Interaction (`internal/aggregation`):**
    *   `NewInfluxDBClient` function creates an InfluxDB v2 client.
    *   `QueryAggregatedData` function constructs and executes a Flux query to fetch `totalFlow`, `totalSeconds`, `avgPSI`, `avgTempF`, `avgAmps`. It includes filtering for valid tags and uses `pivot` for structuring results.
*   **Application Core (`cmd/mwp_data_service/main.go`):**
    *   Parses command-line arguments for configuration path and environment.
    *   Initializes the `WaterDataTable`.
    *   Writes the initial (empty) table to `output/watertable_initial.json` and `output/watertable_initial_report.txt`.
    *   Establishes a connection to InfluxDB.
    *   Performs a data aggregation for a hardcoded time window (e.g., "-24h").
    *   Calculates GPM for each retrieved record.
    *   Updates the `WaterDataTable` with fetched and calculated data.
    *   Writes the updated table to `output/watertable_after_query.json` and `output/watertable_report_after_query.txt`.
*   **Output Formatting (`internal/service/file_writer.go`):**
    *   Provides functions to write the `WaterDataTable` to both JSON and a formatted text file.
    *   The text report displays run time as "TotalMinutes" for better readability.
*   **Build Process:** The application is built using `make` (presumably via CMake calling Go build tools).

**Current Operational Mode:**

The application runs as a command-line tool. It executes the data aggregation sequence once for a hardcoded time window and then exits. It does not yet operate as a continuous service or interact with MQTT.

## 6. Next Steps / Open Questions

The immediate focus is to transition the application from a one-off execution tool to a continuously running service that responds to MQTT requests.

*   **MQTT Integration (High Priority):**
    *   Implement MQTT client setup in `internal/mqtt/client.go` (connect, handle Paho library).
    *   Subscribe to the `request_topic` defined in `config.yaml`.
    *   Implement a message callback (`onMessageReceived`) to:
        *   Parse the incoming MQTT message payload (expected to be a time range string like "24h", "7d", or a simple JSON like `{"range": "24h"}`).
        *   Validate the requested time range against a predefined set of allowed values or formats.
        *   If valid, trigger the data aggregation process (passing the time range).
    *   Implement a function `PublishResults(client MQTTClient, topic string, dataTable datastore.WaterDataTable)` to:
        *   Convert the `WaterDataTable` into the required JSON array format (iterating through all C/Z, creating objects with C, Z, and `ZoneData` fields).
        *   Publish the JSON string to the `response_topic`.
*   **Service Logic & Main Loop (High Priority):**
    *   Refactor `cmd/mwp_data_service/main.go` to:
        *   Initialize components (config, data table, InfluxDB client, MQTT client).
        *   Start the MQTT client and its listeners.
        *   Enter a main service loop that waits for shutdown signals (or relies on MQTT Paho library's internal looping).
    *   Develop `internal/service/service.go` to orchestrate the flow:
        *   On receiving an MQTT request (via a channel or callback from `internal/mqtt`):
            1.  Call `datastore.ResetUpdateStatus(waterDataTable)`.
            2.  Call `aggregation.QueryAggregatedData(influxClient, requestedTimeRange, cfg.InfluxDB)`.
            3.  Loop through query results, calculate GPM, and call `datastore.UpdateEntry(...)`.
            4.  Call `mqtt.PublishResults(...)`.
    *   Implement OS signal handling (SIGINT, SIGTERM) in `main.go` or `internal/service` for graceful shutdown:
        *   Disconnect the MQTT client.
        *   Close the InfluxDB client.
*   **Logging:**
    *   Replace `fmt.Printf` and `fmt.Fprintf(os.Stderr, ...)` with a proper logging solution (e.g., standard `log` package configured for levels and file output based on `cfg.Logging`).
*   **Error Handling and Resilience:**
    *   Review and enhance error handling for InfluxDB queries, MQTT operations, file I/O, etc.
    *   Consider adding retry mechanisms for temporary InfluxDB or MQTT connection issues if the service is intended to be long-running without manual restarts for minor glitches.
*   **Configuration Security:**
    *   The `YOUR_INFLUXDB_TOKEN_HERE` placeholder in `config.yaml` must be addressed. For production, using environment variables to supply the token to the application is strongly recommended over hardcoding it. The application can be modified to read this from an environment variable at startup.
*   **Refinement & Testing:**
    *   Thoroughly test the InfluxDB Flux query with various data scenarios.
    *   Test the MQTT request/response cycle.
    *   Consider adding unit tests for critical functions (e.g., GPM calculation, time range validation).
*   **Deployment & Operation (Later Stage):**
    *   Update/finalize `start_mwp_service.sh` and `stop_mwp_service.sh` scripts to manage the service.
    *   Configure `cron` jobs or a systemd service for robust, persistent operation and auto-restarts if necessary.
*   **Build System:**
    *   Ensure `mwp_data_service/CMakeLists.txt` correctly handles the Go build process, dependencies, and installs the final executable to the `bin/` directory as part of the main project's `make install` target.

## 7. Status of Existing `log_data` Tools (`logit`, `logitdebug`)

It is explicitly intended that the existing Go command-line tools located in the `log_data/` directory (`logit` and `logitdebug`) will **remain separate and functional**.

*   **Purpose:** They will continue to serve as standalone utilities for direct command-line querying of InfluxDB data (aggregated via `logit`, raw via `logitdebug`).
*   **Use Cases:** Useful for quick checks, debugging data issues directly, and comparing results with the new background service during development and operation.
*   **Build Process:** These tools are now built as part of the unified CMake build process defined in `log_data/CMakeLists.txt` and the root `CMakeLists.txt`.
*   **Installation:** They are installed alongside the other project executables into `/home/pi/MWPLog/bin` via `make install`.
*   **Maintenance Note:** If future changes are made to the InfluxDB schema or core aggregation logic within the `mwp_data_service`, corresponding manual updates may be required for `logit` and `logitdebug` if they need to reflect those changes. 