package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// ConfigVersion represents the configuration file version
type ConfigVersion struct {
	Version string `json:"version"`
}

// MigrationService handles configuration format migration
type MigrationService struct {
	configPath string
}

// NewMigrationService creates a new migration service
func NewMigrationService(configPath string) *MigrationService {
	return &MigrationService{
		configPath: configPath,
	}
}

// DetectConfigFormat detects whether the config file is legacy or new format
func (m *MigrationService) DetectConfigFormat() (string, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config file: %w", err)
	}

	// Try to parse as version info
	var version ConfigVersion
	if err := json.Unmarshal(data, &version); err == nil && version.Version != "" {
		return version.Version, nil
	}

	// Check for legacy format fields
	var legacyCheck struct {
		DBHost string `json:"db_host"`
	}
	if err := json.Unmarshal(data, &legacyCheck); err == nil && legacyCheck.DBHost != "" {
		return "1.0", nil
	}

	return "unknown", fmt.Errorf("unknown configuration format")
}

// MigrateToMultiConfig migrates legacy config to new multi-record format
func (m *MigrationService) MigrateToMultiConfig() error {
	version, err := m.DetectConfigFormat()
	if err != nil {
		return fmt.Errorf("failed to detect config format: %w", err)
	}

	switch version {
	case "1.0":
		return m.migrateFromLegacy()
	case "2.0":
		log.Println("Configuration is already in new format")
		return nil
	default:
		return fmt.Errorf("unsupported configuration version: %s", version)
	}
}

// migrateFromLegacy migrates from legacy single-record format
func (m *MigrationService) migrateFromLegacy() error {
	log.Println("Migrating from legacy configuration format...")

	// Backup original config
	if err := m.backupConfig(); err != nil {
		return fmt.Errorf("failed to backup config: %w", err)
	}

	// Load legacy config
	legacyConfig, err := LoadConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to load legacy config: %w", err)
	}

	// Convert to new format
	multiConfig := MigrateFromLegacyConfig(legacyConfig)

	// Save new format
	if err := SaveMultiConfig(m.configPath, multiConfig); err != nil {
		return fmt.Errorf("failed to save migrated config: %w", err)
	}

	log.Println("Configuration migration completed successfully")
	return nil
}

// backupConfig creates a backup of the current configuration
func (m *MigrationService) backupConfig() error {
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup_%s", m.configPath, timestamp)

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config for backup: %w", err)
	}

	err = os.WriteFile(backupPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	log.Printf("Configuration backed up to: %s", backupPath)
	return nil
}

// AutoMigrate automatically detects and migrates configuration if needed
func (m *MigrationService) AutoMigrate() error {
	version, err := m.DetectConfigFormat()
	if err != nil {
		return err
	}

	if version == "1.0" {
		return m.MigrateToMultiConfig()
	}

	return nil
}

// GenerateExampleMultiConfig creates an example multi-record configuration file
func (m *MigrationService) GenerateExampleMultiConfig(outputPath string) error {
	exampleConfig := &MultiConfig{
		Version: "2.0",
		DataSources: []DataSourceConfig{
			{
				ID:           "example-mysql",
				Name:         "Example MySQL Database",
				Type:         "mysql",
				Host:         "127.0.0.1",
				Port:         3306,
				User:         "elasticrelay_user",
				Password:     "elasticrelay_pass",
				Database:     "example_db",
				ServerID:     100,
				TableFilters: []string{"users", "orders"},
				Options: map[string]interface{}{
					"charset": "utf8mb4",
					"timeout": "30s",
				},
			},
		},
		Sinks: []SinkConfig{
			{
				ID:        "example-es",
				Name:      "Example Elasticsearch",
				Type:      "elasticsearch",
				Addresses: []string{"http://localhost:9200"},
				User:      "elastic",
				Password:  "password",
				Options: map[string]interface{}{
					"index_prefix": "elasticrelay",
					"batch_size":   1000,
				},
			},
		},
		Jobs: []SyncJobConfig{
			{
				ID:          "example-job",
				Name:        "Example Sync Job",
				SourceID:    "example-mysql",
				SinkID:      "example-es",
				Enabled:     true,
				Description: "Example data synchronization job",
				Options: map[string]interface{}{
					"initial_sync": true,
					"batch_size":   500,
				},
			},
		},
		Global: GlobalConfig{
			LogLevel:    "info",
			MetricsPort: 8080,
			GRPCPort:    50051,
		},
	}

	return SaveMultiConfig(outputPath, exampleConfig)
}
