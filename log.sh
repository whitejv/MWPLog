#!/bin/bash

# Set library paths for C-based applications
export LD_LIBRARY_PATH=/usr/local/lib:/home/pi/lib:/home/pi/CodeDev/paho.mqtt.c-master/build/output:/home/pi/json-c/json-c-build

# Set the base directory relative to the script location and cd into it
BASE_DIR=$(dirname "$(readlink -f "$0")")
cd "$BASE_DIR" || exit

# Define subdirectories
LOG_DIR="${BASE_DIR}/logs"
BIN_DIR="${BASE_DIR}/bin"
CONFIG_DIR="${BASE_DIR}/config"

mkdir -p "$LOG_DIR"
mkdir -p "${BASE_DIR}/output" # Ensure output directory exists

# Create a timestamped log file for the startup process itself
TIMESTAMP=$(date '+%Y%m%d_%H%M%S')
LOG_FILE="${LOG_DIR}/startup_log_${TIMESTAMP}.log"

# Function to log messages
log_message() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$LOG_FILE"
}

# Start logging
log_message "### Starting Logging Processes from ${BASE_DIR} ###"

# Start mwp_data_service
log_message "Starting MWP Data Service"
nohup ${BIN_DIR}/mwp_data_service -P -c mwp_data_service/config/config.yaml >> "${LOG_DIR}/mwp_data_service.log" 2>&1 &
sleep 5

# Start blynkLog Controller 1
log_message "Starting Blynk Log Controller 1 Interface"
nohup ${BIN_DIR}/blynkLog -P -v -c "${CONFIG_DIR}/logC1_config.json" >> "${LOG_DIR}/blynkLog_C1.log" 2>&1 &
sleep 5

# Start blynkLog Controller 2
log_message "Starting Blynk Log Controller 2 Interface"
nohup ${BIN_DIR}/blynkLog -P -v -c "${CONFIG_DIR}/logC2_config.json" >> "${LOG_DIR}/blynkLog_C2.log" 2>&1 &
sleep 5

log_message "### Logging Startup Complete ###" 