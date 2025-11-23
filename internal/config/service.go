package config

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// ConfigService handles configuration management operations.
type ConfigService struct {
	configPath string
}

// NewConfigService creates a new configuration service.
func NewConfigService(configPath string) *ConfigService {
	return &ConfigService{
		configPath: configPath,
	}
}

// GetCurrentConfig returns the current configuration.
func (s *ConfigService) GetCurrentConfig(ctx context.Context) (*Config, error) {
	config := GetConfig()
	if config == nil {
		// Try to reload from file if not loaded
		var err error
		config, err = LoadConfig(s.configPath)
		if err != nil {
			return nil, err
		}
	}
	return config, nil
}

// UpdateConfiguration updates the configuration and saves it to file.
func (s *ConfigService) UpdateConfiguration(ctx context.Context, newConfig *Config) error {
	if err := UpdateConfig(newConfig); err != nil {
		log.Printf("ConfigService: Failed to update config: %v", err)
		return err
	}
	
	log.Printf("ConfigService: Configuration updated successfully")
	return nil
}

// ReloadConfiguration reloads the configuration from file.
func (s *ConfigService) ReloadConfiguration(ctx context.Context) error {
	// Reset the sync.Once to allow reloading
	cfgMux.Lock()
	once = sync.Once{}
	cfg = nil
	cfgMux.Unlock()
	
	_, err := LoadConfig(s.configPath)
	if err != nil {
		log.Printf("ConfigService: Failed to reload config: %v", err)
		return err
	}
	
	log.Printf("ConfigService: Configuration reloaded successfully")
	return nil
}

// ValidateConfig validates the configuration for correctness.
func (s *ConfigService) ValidateConfig(config *Config) error {
	if config.DBHost == "" {
		return fmt.Errorf("db_host cannot be empty")
	}
	if config.DBPort <= 0 || config.DBPort > 65535 {
		return fmt.Errorf("db_port must be between 1 and 65535")
	}
	if config.DBUser == "" {
		return fmt.Errorf("db_user cannot be empty")
	}
	if config.DBName == "" {
		return fmt.Errorf("db_name cannot be empty")
	}
	if len(config.ESAddresses) == 0 {
		return fmt.Errorf("es_addresses cannot be empty")
	}
	return nil
}
