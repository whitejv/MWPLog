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
    *   It receives user input/commands (like time window selection via a datastream, e.g., `downlink/ds/TimeWindow`) *from* the Blynk Platform.
    *   It translates these commands into standardized MQTT request messages (e.g., `{"range": "12h"}`).
    *   It publishes these request messages to the `mwp_data_service`'s request topic on the local MQTT Broker (e.g., `mwp/json/data/log/dataservice/query_request`).
    *   It subscribes to the `mwp_data_service`'s response topic on the local MQTT Broker (e.g., `mwp/json/data/log/dataservice/query_results`).
    *   It receives the full JSON watertable results from the `mwp_data_service`.
    *   **(Future Work)** It will parse this JSON and update up to 155 specific Blynk datastreams via the Blynk Platform, corresponding to 31 predefined Controller/Zone rows of data.
5.  **MWP Data Aggregation Service (This Go App):** The core component detailed in this plan. It connects *only* to the local MQTT Broker and InfluxDB.
    *   Subscribes to the request topic on the local MQTT Broker (e.g., `mwp/json/data/log/dataservice/query_request`) to receive time window selection.
    *   Processes requests received from the `BlynkLog` app.
    *   Aggregates data from InfluxDB based on the received time window.
    *   Maintains the static internal state (`WaterDataTable`).
    *   Publishes results (full `WaterDataTable` as JSON) to the response topic on the local MQTT Broker (e.g., `mwp/json/data/log/dataservice/query_results`).
    *   Generates `output/watertable_latest_report.txt`, now including the active time window at the top.
6.  **InfluxDB:** (External) The time-series database storing the raw sensor data from the MWP system.

**Communication Flow:**

```
+-----------+      Blynk       +-----------------+      MQTT Request       +---------------------+      InfluxDB       +-----------+
| Blynk GUI | <~~> Platform <~~> | BlynkLog C App  | ~~~~> (Request Topic) ~~~~> | Go Service (This App) | ~~~> Query ~~~> | InfluxDB  |
|  (User)   |  (Time Window DS) | (Parse Blynk DS, | (e.g. "mwp/json/data/  |  (Receives Time      | (Uses Time     |           |
+-----------+                   |  Format & Pub   |  log/dataservice/     |   Window Request)   |  Window)       |           |
                                |  Time Window)   |  query_request")      |                     |                |           |
                                +-----------------+                       +---------------------+ <~~ Result ~~~    +-----------+
                                       |         Local MQTT Broker           |        (Local)
         Blynk Platform                |                                     |
         (155 Datastreams              | MQTT Response                       |
          for table data)              | (Response Topic)                    |
               ^                       | (e.g. "mwp/json/data/log/          |
               |                       |  dataservice/query_results")        |
               |                       +-------------------------------------+\
               +----------------------- (Subscribes, Parses Full JSON,       |
                                        Populates Internal Table for Blynk)  |
                                        (Future: Publishes to 155 DS)        |
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
    *   Sets up MQTT client, subscribes to `mqttConfig.RequestTopic` (e.g., `mwp/json/data/log/dataservice/query_request`) and defines `mqttMessageHandler` to process incoming time window requests.
    *   The `mqttMessageHandler` triggers `processAndPublishData` with the received time window.
    *   `processAndPublishData` function:
        1.  Queries InfluxDB using the provided `timeRange`.
        2.  Updates the internal `WaterDataTable`.
        3.  Publishes the complete `ReportData` (containing `WaterDataTable`) as a JSON string to `mqttConfig.ResponseTopic` (e.g., `mwp/json/data/log/dataservice/query_results`).
        4.  Writes `output/watertable_latest.json`.
        5.  Writes `output/watertable_latest_report.txt`, now prepended with the `timeRange` used for the query.
    *   Initial data load on startup uses a command-line specified or default `initialQueryRange`.
    *   Handles OS signals for graceful shutdown.
*   **`internal/service/file_writer.go`:**
    *   `WriteDataTableToJSON`: Writes the `ReportData` to a JSON file.
    *   `WriteDataTableAsTextReport`: Function signature updated to `WriteDataTableAsTextReport(..., timeWindow string)`. It now prepends `Time Window: <timeWindow>` to the human-readable text report.
*   *(Planned for `internal/service/service.go`): Further refinements to service orchestration if needed beyond `main.go`.*

## 5. Development Progress & Current Status

The system has progressed significantly, with both `mwp_data_service` and `blynkLog.c` achieving key milestones in their interaction.

**`mwp_data_service` Implemented Features:**

*   **Project Structure, Configuration, Data Storage, InfluxDB Interaction:** Largely as previously documented in section 4.
*   **MQTT Integration:**
    *   Connects to MQTT broker based on environment settings (dev/prod) defined in `config.yaml`.
    *   Subscribes to `mqttConfig.RequestTopic` (e.g., `mwp/json/data/log/dataservice/query_request`) from `config.yaml`.
    *   `mqttMessageHandler` in `cmd/mwp_data_service/main.go` receives JSON payloads like `{"range": "12h"}`, extracts the time window, and triggers data processing.
*   **Core Logic (`cmd/mwp_data_service/main.go`):**
    *   `processAndPublishData(timeRange string)` function:
        *   Queries InfluxDB using the provided `timeRange`.
        *   Updates the internal `WaterDataTable`.
        *   Publishes the complete `ReportData` (which includes `Details: waterDataTable` and super summaries) as a JSON string to `mqttConfig.ResponseTopic` (e.g., `mwp/json/data/log/dataservice/query_results`) from `config.yaml`.
*   **Output Formatting (`internal/service/file_writer.go`):**
    *   `WriteDataTableAsTextReport` function now accepts the current `timeWindow` as a parameter and prepends "Time Window: [timeWindow]" to `output/watertable_latest_report.txt` and `output/watertable_initial_report.txt`.
    *   `WriteDataTableToJSON` writes a `ReportData` struct (including summaries and details) to `output/watertable_latest.json`.
*   **"Now" Mode Functionality:**
    *   Implemented with a `nowModeManager` goroutine, allowing frequent updates (e.g., every 1 second for `"-1m"` data) for a configurable duration (e.g., 60 seconds), after which it reverts to the last specified or default range.
    *   Activated by an MQTT request with `{"range": "now"}`.
*   **Operational Mode:** Runs as a continuous service, performing an initial data load and then responding to MQTT requests for different time windows or operating in "now" mode. Handles OS signals for graceful shutdown.

**`BlynkLog.c` Implemented Features (relevant to `mwp_data_service` interaction):**

*   **Time Window Selection & Publishing to `mwp_data_service`:**
    *   Subscribes to a Blynk datastream (e.g., `downlink/ds/TimeWindow`) to receive an integer representing a user-selected time window.
    *   Maps this integer to a predefined time window string (e.g., "1h", "12h", "May") using `timeWindowMap`.
    *   Formats this string into a JSON payload (e.g., `{"range": "12h"}`).
    *   Publishes this JSON to `mwp_data_service`'s request topic (`MWP_DATA_SERVICE_QUERY_TOPIC`, e.g., `mwp/json/data/log/dataservice/query_request`).
*   **Receiving and Parsing Watertable Data from `mwp_data_service`:**
    *   Subscribes to `mwp_data_service`'s response topic (`MWP_WATERTABLE_JSON_TOPIC`, e.g., `mwp/json/data/log/dataservice/query_results`).
    *   The `msgarrvd` callback for the main `client`:
        *   Parses the incoming JSON payload from `mwp_data_service`.
        *   Navigates the JSON structure (specifically `parsed_json.details.controller_key.zone_key`).
        *   Populates an internal C array (`static BlynkDataRow blynk_display_table[31]`) with data for 31 predefined Controller/Zone combinations defined in `json_to_blynk_map`.
        *   Each entry in `blynk_display_table` stores: `zone_number`, `total_flow`, `total_minutes` (calculated), `avg_psi`, `gpm`, and a `data_valid` flag.
*   **Error Handling:** Basic JSON parsing error checks are in place.
*   **MQTT Client Management:** Separate MQTT clients for Blynk cloud (`blynkClient`) and local broker (`client`) are managed, including connection, callbacks, and subscription logic.

**Current System State:**

*   The data pipeline for time window selection and data retrieval is functional:
    1.  User selects time window in Blynk App.
    2.  `BlynkLog.c` receives this, translates it, and sends it as an MQTT request to `mwp_data_service`.
    3.  `mwp_data_service` receives the request, queries InfluxDB using the specified time window.
    4.  `mwp_data_service` generates `watertable_latest_report.txt` (including the time window header) and `watertable_latest.json`.
    5.  `mwp_data_service` publishes the full watertable JSON data to another MQTT topic.
    6.  `BlynkLog.c` receives this JSON, parses it, and populates its internal `blynk_display_table` with the 31 specified rows of data.
*   Both applications are in a stable state regarding this interaction.

## 6. Next Steps / Open Questions

**For `BlynkLog.c` (High Priority):**

*   **Publish Table Data to Blynk Datastreams:**
    *   **Define Datastream Aliases:** Finalize the exact naming convention for the 155 Blynk datastream aliases that will be created in the Blynk console (e.g., pattern `C{controller_idx}Z{zone_idx}_{metric_suffix}` like `C0Z0_zone`, `C0Z0_flow`, etc.).
    *   **Define Blynk Publishing Base Topic:** Specify the MQTT topic prefix for publishing to these datastreams (e.g., `"ds"` or an equivalent to the `BLYNK_DS` macro used in prior examples).
    *   **Implement Publishing Logic:**
        *   Create a new function or modify the main loop/`msgarrvd` to iterate through the `blynk_display_table` (31 rows) after it has been populated.
        *   For each row where `data_valid` is true:
            *   Format each of the 5 data values (zone number, total flow, total minutes, avg psi, gpm) as a string.
            *   Construct the full MQTT topic for each of the 5 corresponding Blynk datastream aliases for that row.
            *   Publish these 5 values to their respective datastreams using the `blynkClient`.
    *   **Handling Invalid Data:** Decide how to represent rows in Blynk if `data_valid` is false for an entry in `blynk_display_table` (e.g., send "0", a specific placeholder like "N/A", or skip updating those datastreams).

**For `mwp_data_service` (Lower Priority / Refinements):**

*   **Logging:** Transition from `fmt.Printf` and `fmt.Fprintf(os.Stderr, ...)` to a more structured logging solution (e.g., standard Go `log` package or a third-party library) for better log management, levels, and potential file output based on `cfg.Logging`.
*   **Error Handling and Resilience:** Review and potentially enhance error handling for InfluxDB queries, MQTT operations. Paho's auto-reconnect helps, but application-level resilience could be reviewed.
*   **Configuration Security (InfluxDB Token):** Emphasize using environment variables for sensitive data like the InfluxDB token in production, rather than hardcoding in `config.yaml`.
*   **Testing:** Expand testing, potentially including unit tests for critical logic.

**General:**

*   Perform thorough end-to-end testing of the complete data flow from Blynk UI to `mwp_data_service` and back to Blynk UI (once `BlynkLog.c` publishing is implemented).
*   Review and update deployment scripts (`start_mwp_service.sh`, etc.) for the continuous operational mode of `mwp_data_service`.
*   Consider using systemd for managing the `mwp_data_service` and `blynkLog` processes for robustness and auto-restarts.

## 7. Status of Existing `log_data` Tools (`logit`, `logitdebug`)

It is explicitly intended that the existing Go command-line tools located in the `log_data/` directory (`logit` and `logitdebug`) will **remain separate and functional**.

*   **Purpose:** They will continue to serve as standalone utilities for direct command-line querying of InfluxDB data (aggregated via `logit`, raw via `logitdebug`).
*   **Use Cases:** Useful for quick checks, debugging data issues directly, and comparing results with the new background service during development and operation.
*   **Build Process:** These tools are now built as part of the unified CMake build process defined in `log_data/CMakeLists.txt` and the root `CMakeLists.txt`.
*   **Installation:** They are installed alongside the other project executables into `/home/pi/MWPLog/bin` via `make install`.
*   **Maintenance Note:** If future changes are made to the InfluxDB schema or core aggregation logic within the `mwp_data_service`, corresponding manual updates may be required for `logit` and `logitdebug` if they need to reflect those changes.