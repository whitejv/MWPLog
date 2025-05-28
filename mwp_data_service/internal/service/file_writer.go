package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"mwp_data_service/internal/datastore" // To use datastore.WaterDataTable
)

// WriteDataTableToJSON marshals the WaterDataTable to an indented JSON format
// and writes it to the specified filename. It creates the output directory if it doesn't exist.
func WriteDataTableToJSON(dataTable datastore.WaterDataTable, filename string) error {
	// Ensure the output directory exists
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" { // Check if a directory part exists
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory '%s': %w", dir, err)
		}
	}

	// Marshal the data table to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(dataTable, "", "  ") // Using two spaces for indentation
	if err != nil {
		return fmt.Errorf("failed to marshal WaterDataTable to JSON: %w", err)
	}

	// Write the JSON data to the file
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON data to file '%s': %w", filename, err)
	}

	fmt.Printf("Successfully wrote water data table to %s\n", filename)
	return nil
}

// WriteDataTableAsTextReport formats the WaterDataTable into a human-readable text report
// and writes it to the specified filename. It creates the output directory if it doesn't exist.
func WriteDataTableAsTextReport(dataTable datastore.WaterDataTable, filename string) error {
	// Ensure the output directory exists
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" { // Check if a directory part exists
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory '%s': %w", dir, err)
		}
	}

	var report strings.Builder

	// Define header with padding - adjust padding as needed
	header := fmt.Sprintf("%-12s %-6s %-10s %-12s %-8s %-10s %-10s %-8s %-8s\n",
		"Controller", "Zone", "TotalFlow", "TotalMinutes", "AvgPSI", "AvgTempF", "AvgAmps", "GPM", "Updated")
	report.WriteString(header)
	report.WriteString(strings.Repeat("-", len(header)-1) + "\n") // Separator line

	// Get controller IDs and sort them for consistent output
	controllerIDs := make([]string, 0, len(dataTable))
	for id := range dataTable {
		controllerIDs = append(controllerIDs, id)
	}
	sort.Strings(controllerIDs)

	for _, controllerID := range controllerIDs {
		zonesMap := dataTable[controllerID]
		zoneIDs := make([]string, 0, len(zonesMap))
		for id := range zonesMap {
			zoneIDs = append(zoneIDs, id)
		}
		// Sort zone IDs numerically for correct order (e.g., "1", "2", ..., "10")
		sort.Slice(zoneIDs, func(i, j int) bool {
			iVal, _ := strconv.Atoi(zoneIDs[i])
			jVal, _ := strconv.Atoi(zoneIDs[j])
			return iVal < jVal
		})

		for _, zoneID := range zoneIDs {
			data := zonesMap[zoneID]
			totalMinutes := data.TotalSeconds / 60.0
			row := fmt.Sprintf("%-12s %-6s %-10.2f %-12.2f %-8.2f %-10.2f %-10.3f %-8.2f %-8t\n",
				controllerID, zoneID,
				data.TotalFlow, totalMinutes, data.AvgPSI,
				data.AvgTempF, data.AvgAmps, data.GPM,
				data.UpdatedInLastQuery)
			report.WriteString(row)
		}
	}

	// Write the report to the file
	if err := os.WriteFile(filename, []byte(report.String()), 0644); err != nil {
		return fmt.Errorf("failed to write text report to file '%s': %w", filename, err)
	}

	fmt.Printf("Successfully wrote water data table text report to %s\n", filename)
	return nil
}
