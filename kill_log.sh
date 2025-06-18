#!/bin/bash

# Set the base and log directories relative to the script location
BASE_DIR=$(dirname "$(readlink -f "$0")")
LOG_DIR="${BASE_DIR}/logs"
mkdir -p "$LOG_DIR"

# Create a timestamped log file
TIMESTAMP=$(date '+%Y%m%d_%H%M%S')
KILL_LOG="${LOG_DIR}/kill_log_${TIMESTAMP}.log"

# Function to log messages
log_message() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$KILL_LOG"
}

log_message "### Stopping Logging Processes ###"

# List of process names to kill
PROCESSES=(
    "blynkLog"
    "mwp_data_service"
)

# Kill each process
for proc in "${PROCESSES[@]}"; do
    log_message "Stopping $proc"
    pkill -f "$proc"
    sleep 1
done

log_message "All processes stopped."

# Verify nothing is left running
log_message "Checking for remaining processes..."
ps -ef | grep -E "$(printf "%s|" "${PROCESSES[@]}")" | grep -v grep >> "$KILL_LOG"

log_message "### Kill Process Complete ###" 