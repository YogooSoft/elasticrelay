
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Config holds the full application configuration.
type Config struct {
	DBHost       string   `json:"db_host"`
	DBPort       int      `json:"db_port"`
	DBUser       string   `json:"db_user"`
	DBPassword   string   `json:"db_password"`
	DBName       string   `json:"db_name"`
	ServerID     uint32   `json:"server_id"`
	TableFilters []string `json:"table_filters"`
	ESAddresses  []string `json:"es_addresses"`
	ESUser       string   `json:"es_user"`
	ESPassword   string   `json:"es_password"`
}

var (
	cfg     *Config
	once    sync.Once
	cfgMux  sync.RWMutex
	cfgPath string
)

// LoadConfig reads the configuration from a file and unmarshals it.
// It uses a sync.Once to ensure the file is only read once.
func LoadConfig(path string) (*Config, error) {
	var err error
	once.Do(func() {
		cfgPath = path
		var data []byte
		data, err = os.ReadFile(path)
		if err != nil {
			err = fmt.Errorf("failed to read config file %s: %w", path, err)
			return
		}

		var tempCfg Config
		if err = json.Unmarshal(data, &tempCfg); err != nil {
			err = fmt.Errorf("failed to parse config file %s: %w", path, err)
			return
		}
		cfg = &tempCfg
	})

	if err != nil {
		// Reset sync.Once if initialization failed, allowing retry.
		once = sync.Once{}
		return nil, err
	}

	return cfg, nil
}

// SaveConfig writes the configuration to a file.
func SaveConfig(path string, config *Config) error {
	cfgMux.Lock()
	defer cfgMux.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = os.WriteFile(path, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}

	// Update the in-memory configuration
	cfg = config
	return nil
}

// UpdateConfig updates the current configuration and saves it to file.
func UpdateConfig(config *Config) error {
	if cfgPath == "" {
		return fmt.Errorf("config path not initialized, call LoadConfig first")
	}
	return SaveConfig(cfgPath, config)
}

// GetConfig returns the current configuration (thread-safe read).
func GetConfig() *Config {
	cfgMux.RLock()
	defer cfgMux.RUnlock()
	
	if cfg == nil {
		return nil
	}
	
	// Return a copy to prevent modifications
	configCopy := *cfg
	return &configCopy
}
