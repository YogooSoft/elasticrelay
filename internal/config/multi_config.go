package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// DataSourceConfig represents a single data source configuration
type DataSourceConfig struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"` // mysql, postgresql, mongodb
	Host         string                 `json:"host"`
	Port         int                    `json:"port"`
	User         string                 `json:"user"`
	Password     string                 `json:"password"`
	Database     string                 `json:"database"`
	ServerID     uint32                 `json:"server_id,omitempty"`     // for MySQL replication
	TableFilters []string               `json:"table_filters,omitempty"` // for table filtering
	Options      map[string]interface{} `json:"options,omitempty"`       // additional options
}

// SinkConfig represents a sink destination configuration
type SinkConfig struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"` // elasticsearch, kafka, etc.
	Addresses []string               `json:"addresses"`
	User      string                 `json:"user,omitempty"`
	Password  string                 `json:"password,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
}

// SyncJobConfig represents a synchronization job configuration
type SyncJobConfig struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	SourceID    string                 `json:"source_id"` // references DataSourceConfig.ID
	SinkID      string                 `json:"sink_id"`   // references SinkConfig.ID
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// MultiConfig represents the new multi-record configuration structure
type MultiConfig struct {
	Version     string             `json:"version"`
	DataSources []DataSourceConfig `json:"data_sources"`
	Sinks       []SinkConfig       `json:"sinks"`
	Jobs        []SyncJobConfig    `json:"jobs"`
	Global      GlobalConfig       `json:"global"`
}

// DLQConfig represents Dead Letter Queue configuration
type DLQConfig struct {
	Enabled     bool   `json:"enabled"`
	StoragePath string `json:"storage_path"`
	MaxRetries  int    `json:"max_retries"`
	RetryDelay  string `json:"retry_delay"`
}

// GlobalConfig represents global application settings
type GlobalConfig struct {
	LogLevel    string     `json:"log_level"`
	MetricsPort int        `json:"metrics_port,omitempty"`
	GRPCPort    int        `json:"grpc_port,omitempty"`
	DLQConfig   *DLQConfig `json:"dlq_config,omitempty"`
}

var (
	multiCfg     *MultiConfig
	multiOnce    sync.Once
	multiCfgMux  sync.RWMutex
	multiCfgPath string
)

// LoadMultiConfig loads the multi-record configuration
func LoadMultiConfig(path string) (*MultiConfig, error) {
	var err error
	multiOnce.Do(func() {
		multiCfgPath = path
		var data []byte
		data, err = os.ReadFile(path)
		if err != nil {
			err = fmt.Errorf("failed to read multi config file %s: %w", path, err)
			return
		}

		var tempCfg MultiConfig
		if err = json.Unmarshal(data, &tempCfg); err != nil {
			err = fmt.Errorf("failed to parse multi config file %s: %w", path, err)
			return
		}
		multiCfg = &tempCfg
	})

	if err != nil {
		multiOnce = sync.Once{}
		return nil, err
	}

	return multiCfg, nil
}

// SaveMultiConfig saves the multi-record configuration to file
func SaveMultiConfig(path string, config *MultiConfig) error {
	multiCfgMux.Lock()
	defer multiCfgMux.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal multi config: %w", err)
	}

	err = os.WriteFile(path, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write multi config file %s: %w", path, err)
	}

	multiCfg = config
	return nil
}

// UpdateMultiConfig updates the current multi-record configuration
func UpdateMultiConfig(config *MultiConfig) error {
	if multiCfgPath == "" {
		return fmt.Errorf("multi config path not initialized")
	}
	return SaveMultiConfig(multiCfgPath, config)
}

// GetMultiConfig returns the current multi-record configuration (thread-safe)
func GetMultiConfig() *MultiConfig {
	multiCfgMux.RLock()
	defer multiCfgMux.RUnlock()

	if multiCfg == nil {
		return nil
	}

	// Return a deep copy
	configCopy := *multiCfg

	// Deep copy slices
	configCopy.DataSources = make([]DataSourceConfig, len(multiCfg.DataSources))
	copy(configCopy.DataSources, multiCfg.DataSources)

	configCopy.Sinks = make([]SinkConfig, len(multiCfg.Sinks))
	copy(configCopy.Sinks, multiCfg.Sinks)

	configCopy.Jobs = make([]SyncJobConfig, len(multiCfg.Jobs))
	copy(configCopy.Jobs, multiCfg.Jobs)

	return &configCopy
}

// AddDataSource adds a new data source configuration
func AddDataSource(source DataSourceConfig) error {
	config := GetMultiConfig()
	if config == nil {
		return fmt.Errorf("multi config not loaded")
	}

	// Check for duplicate ID
	for _, existing := range config.DataSources {
		if existing.ID == source.ID {
			return fmt.Errorf("data source with ID %s already exists", source.ID)
		}
	}

	config.DataSources = append(config.DataSources, source)
	return UpdateMultiConfig(config)
}

// RemoveDataSource removes a data source configuration by ID
func RemoveDataSource(id string) error {
	config := GetMultiConfig()
	if config == nil {
		return fmt.Errorf("multi config not loaded")
	}

	for i, source := range config.DataSources {
		if source.ID == id {
			config.DataSources = append(config.DataSources[:i], config.DataSources[i+1:]...)
			return UpdateMultiConfig(config)
		}
	}

	return fmt.Errorf("data source with ID %s not found", id)
}

// GetDataSource returns a data source configuration by ID
func GetDataSource(id string) (*DataSourceConfig, error) {
	config := GetMultiConfig()
	if config == nil {
		return nil, fmt.Errorf("multi config not loaded")
	}

	for _, source := range config.DataSources {
		if source.ID == id {
			return &source, nil
		}
	}

	return nil, fmt.Errorf("data source with ID %s not found", id)
}

// MigrateFromLegacyConfig converts old single-record config to new multi-record format
func MigrateFromLegacyConfig(legacyConfig *Config) *MultiConfig {
	multiConfig := &MultiConfig{
		Version: "2.0",
		DataSources: []DataSourceConfig{
			{
				ID:           "default-mysql",
				Name:         "Default MySQL Database",
				Type:         "mysql",
				Host:         legacyConfig.DBHost,
				Port:         legacyConfig.DBPort,
				User:         legacyConfig.DBUser,
				Password:     legacyConfig.DBPassword,
				Database:     legacyConfig.DBName,
				ServerID:     legacyConfig.ServerID,
				TableFilters: legacyConfig.TableFilters,
			},
		},
		Sinks: []SinkConfig{
			{
				ID:        "default-es",
				Name:      "Default Elasticsearch",
				Type:      "elasticsearch",
				Addresses: legacyConfig.ESAddresses,
				User:      legacyConfig.ESUser,
				Password:  legacyConfig.ESPassword,
			},
		},
		Jobs: []SyncJobConfig{
			{
				ID:          "default-job",
				Name:        "Default Sync Job",
				SourceID:    "default-mysql",
				SinkID:      "default-es",
				Enabled:     true,
				Description: "Migrated from legacy configuration",
			},
		},
		Global: GlobalConfig{
			LogLevel:    "info",
			MetricsPort: 8080,
			GRPCPort:    50051,
		},
	}

	return multiConfig
}
