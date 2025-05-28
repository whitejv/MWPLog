package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// --- Configuration Structs ---

// Config holds the top-level configuration for the service
type Config struct {
	InfluxDB     InfluxDBConfig               `yaml:"influxdb"`
	Logging      LoggingConfig                `yaml:"logging"`
	Controllers  ControllerConfig             `yaml:"controllers"`
	Environments map[string]EnvironmentConfig `yaml:"environments"` // Key: "development" or "production"
}

// InfluxDBConfig holds InfluxDB connection details
type InfluxDBConfig struct {
	Host   string `yaml:"host"`
	Token  string `yaml:"token"`
	Org    string `yaml:"org"`
	Bucket string `yaml:"bucket"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file,omitempty"` // Optional log file path
}

// ControllerConfig holds details about controller/zone structure
type ControllerConfig struct {
	DefaultZones       int            `yaml:"default_zones"`
	SpecialControllers map[string]int `yaml:"special_controllers"` // Key: ControllerID (string), Value: ZoneCount
	ZoneStartIndex     int            `yaml:"zone_start_index"`
	TotalControllers   int            `yaml:"total_controllers"` // Added field for total number of controllers
}

// EnvironmentConfig holds environment-specific settings (currently only MQTT)
type EnvironmentConfig struct {
	MQTT MQTTConfig `yaml:"mqtt"`
}

// MQTTConfig holds MQTT connection details
type MQTTConfig struct {
	Broker        string `yaml:"broker"`
	ClientID      string `yaml:"client_id"`
	RequestTopic  string `yaml:"request_topic"`
	ResponseTopic string `yaml:"response_topic"`
	Username      string `yaml:"username,omitempty"` // Optional
	Password      string `yaml:"password,omitempty"` // Optional
}

// --- Loading Function ---

// LoadConfig reads the configuration file from the given path and unmarshals it
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{} // Initialize with default Go zero-values

	// Read the file content
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", path, err)
	}

	// Unmarshal the YAML content into the Config struct
	err = yaml.Unmarshal(yamlFile, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file '%s': %w", path, err)
	}

	// Basic validation (add more as needed)
	if cfg.InfluxDB.Host == "" || cfg.InfluxDB.Token == "" || cfg.InfluxDB.Org == "" || cfg.InfluxDB.Bucket == "" {
		return nil, fmt.Errorf("influxdb configuration (host, token, org, bucket) must be set in '%s'", path)
	}
	if _, ok := cfg.Environments["development"]; !ok {
		return nil, fmt.Errorf("missing 'development' environment configuration in '%s'", path)
	}
	if _, ok := cfg.Environments["production"]; !ok {
		return nil, fmt.Errorf("missing 'production' environment configuration in '%s'", path)
	}

	return cfg, nil
}
