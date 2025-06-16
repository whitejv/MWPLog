package datastore

import (
	"strconv"
	// Import the actual config package where ControllerProps will be defined.
	// Adjust the import path if your module structure is different or the package name varies.
	"mwp_data_service/internal/config"
)

// ZoneData holds the aggregated metrics for a specific controller and zone.
type ZoneData struct {
	TotalFlow          float64 `json:"totalFlow"`
	TotalSeconds       float64 `json:"totalSeconds"`
	AvgPSI             float64 `json:"avgPSI"`
	AvgTempF           float64 `json:"avgTempF"`
	AvgAmps            float64 `json:"avgAmps"`
	GPM                float64 `json:"gpm"`                // Calculated Gallons Per Minute
	UpdatedInLastQuery bool    `json:"updatedInLastQuery"` // Tracks if data was updated in the last query cycle
}

// WaterDataTable represents the entire dataset, mapping controller IDs to their zones,
// and then zone IDs to their ZoneData.
// Controller IDs and Zone IDs are stored as strings.
type WaterDataTable map[string]map[string]ZoneData

// InitializeTable creates and initializes the WaterDataTable based on the controller configuration.
// It populates entries for all configured controllers and their respective zones with default (zeroed) ZoneData.
// It now expects cfg to be of type config.ControllerConfig (defined in internal/config/config.go)
func InitializeTable(cfg config.ControllerConfig) WaterDataTable {
	table := make(WaterDataTable)

	for i := 0; i < cfg.TotalControllers; i++ { // Iterate from controller 0 up to TotalControllers-1
		controllerIDStr := strconv.Itoa(i)
		table[controllerIDStr] = make(map[string]ZoneData)

		numZones := cfg.DefaultZones
		if zones, ok := cfg.SpecialControllers[controllerIDStr]; ok {
			numZones = zones
		}

		// Determine the starting zone index for the current controller
		currentZoneStartIndex := cfg.ZoneStartIndex
		if controllerIDStr == "0" { // Controller 0 is special
			currentZoneStartIndex = 0 // Zone 0 is valid for Controller 0
		}

		for j := 0; j < numZones; j++ {
			zoneID := currentZoneStartIndex + j
			zoneIDStr := strconv.Itoa(zoneID)
			table[controllerIDStr][zoneIDStr] = ZoneData{
				// All float64 fields default to 0.0
				// All bool fields default to false
			}
		}
	}
	return table
}

// ResetUpdateStatus resets the UpdatedInLastQuery flag for all entries in the table.
// This should be called before processing new query results.
func ResetUpdateStatus(table WaterDataTable) {
	for _, zones := range table {
		for zoneID, data := range zones {
			// Zero out all data values
			data.TotalFlow = 0.0
			data.TotalSeconds = 0.0
			data.AvgPSI = 0.0
			data.AvgTempF = 0.0
			data.AvgAmps = 0.0
			data.GPM = 0.0
			data.UpdatedInLastQuery = false
			zones[zoneID] = data // Important: update the map with the modified struct
		}
	}
}

// UpdateEntry updates a specific controller/zone with new metrics and marks it as updated.
// If the controller or zone doesn't exist, it does nothing (or could be modified to create it).
// The 'metrics' parameter would be a struct or map containing the values from an InfluxDB query row.
// For now, we'll use direct parameters for key metrics.
func UpdateEntry(table WaterDataTable, controllerID, zoneID string,
	totalFlow, totalSeconds, avgPSI, avgTempF, avgAmps, gpm float64) {

	controllerData, cExists := table[controllerID]
	if !cExists {
		// Controller not found, handle as error or log, or create if desired
		// fmt.Printf("Warning: Controller ID %s not found in WaterDataTable\n", controllerID)
		return
	}

	_, zExists := controllerData[zoneID]
	if !zExists {
		// Zone not found for this controller, handle as error or log, or create if desired
		// fmt.Printf("Warning: Zone ID %s for Controller %s not found in WaterDataTable\n", zoneID, controllerID)
		return
	}

	table[controllerID][zoneID] = ZoneData{
		TotalFlow:          totalFlow,
		TotalSeconds:       totalSeconds,
		AvgPSI:             avgPSI,
		AvgTempF:           avgTempF,
		AvgAmps:            avgAmps,
		GPM:                gpm,
		UpdatedInLastQuery: true,
	}
}
