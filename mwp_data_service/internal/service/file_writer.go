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

// ReportData encapsulates the super summaries and the main data table for JSON output.
type ReportData struct {
	TotalIrrigationGallons float64                  `json:"totalIrrigationGallons"`
	TotalWell3Gallons      float64                  `json:"totalWell3Gallons"`
	Details                datastore.WaterDataTable `json:"details"`
}

// WriteDataTableToJSON marshals the ReportData (including super summaries and WaterDataTable)
// to an indented JSON format and writes it to the specified filename.
func WriteDataTableToJSON(dataTable datastore.WaterDataTable, totalIrrigationGallons, totalWell3Gallons float64, filename string) error {
	// Ensure the output directory exists
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" { // Check if a directory part exists
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory '%s': %w", dir, err)
		}
	}

	// Create the encompassing structure
	reportOutput := ReportData{
		TotalIrrigationGallons: totalIrrigationGallons,
		TotalWell3Gallons:      totalWell3Gallons,
		Details:                dataTable,
	}

	// Marshal the report data to JSON with indentation
	jsonData, err := json.MarshalIndent(reportOutput, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal ReportData to JSON: %w", err)
	}

	// Write the JSON data to the file
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON data to file '%s': %w", filename, err)
	}

	fmt.Printf("Successfully wrote water data table to %s\n", filename)
	return nil
}

// WriteDataTableAsTextReport formats the WaterDataTable into a human-readable text report,
// prepending it with super summaries, and writes it to the specified filename.
func WriteDataTableAsTextReport(dataTable datastore.WaterDataTable, totalIrrigationGallons, totalWell3Gallons float64, filename string) error {
	// Ensure the output directory exists
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" { // Check if a directory part exists
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory '%s': %w", dir, err)
		}
	}

	var report strings.Builder

	// Add Super Summaries at the top
	report.WriteString(fmt.Sprintf("Super Summary - Total Irrigation Gallons (C0, C1, C2): %.2f\n", totalIrrigationGallons))
	report.WriteString(fmt.Sprintf("Super Summary - Total Well 3 Gallons (C3Z1): %.2f\n", totalWell3Gallons))
	report.WriteString("\n" + strings.Repeat("=", 80) + "\n\n") // Separator

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
