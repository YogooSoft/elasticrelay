package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ConfigSyncService handles synchronization between traditional and multi configurations
type ConfigSyncService struct {
	configPath      string
	multiConfigPath string
	mu              sync.RWMutex
	lastSync        time.Time
	autoSync        bool
}

// NewConfigSyncService creates a new configuration synchronization service
func NewConfigSyncService(configPath, multiConfigPath string, autoSync bool) *ConfigSyncService {
	return &ConfigSyncService{
		configPath:      configPath,
		multiConfigPath: multiConfigPath,
		autoSync:        autoSync,
	}
}

// SyncConfigToMulti converts and syncs traditional config to multi config format
func (s *ConfigSyncService) SyncConfigToMulti(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("ConfigSync: Starting sync from config.json to multi_config.json")

	// Load traditional config
	config, err := LoadConfig(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to load traditional config: %w", err)
	}

	// Convert to multi config format
	multiConfig := MigrateFromLegacyConfig(config)

	// Check if multi config file exists and merge if needed
	if _, err := os.Stat(s.multiConfigPath); err == nil {
		existingMultiConfig, err := LoadMultiConfig(s.multiConfigPath)
		if err == nil {
			// Merge with existing multi config, preserving additional data sources
			multiConfig = s.mergeConfigs(multiConfig, existingMultiConfig)
		}
	}

	// Save multi config
	if err := SaveMultiConfig(s.multiConfigPath, multiConfig); err != nil {
		return fmt.Errorf("failed to save multi config: %w", err)
	}

	s.lastSync = time.Now()
	log.Printf("ConfigSync: Successfully synced config.json to multi_config.json")
	return nil
}

// SyncMultiToConfig converts and syncs multi config to traditional config format
func (s *ConfigSyncService) SyncMultiToConfig(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("ConfigSync: Starting sync from multi_config.json to config.json")

	// Load multi config
	multiConfig, err := LoadMultiConfig(s.multiConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load multi config: %w", err)
	}

	// Convert to traditional config format (using first data source and sink)
	config, err := s.convertMultiToLegacy(multiConfig)
	if err != nil {
		return fmt.Errorf("failed to convert multi config to legacy: %w", err)
	}

	// Save traditional config
	if err := SaveConfig(s.configPath, config); err != nil {
		return fmt.Errorf("failed to save traditional config: %w", err)
	}

	s.lastSync = time.Now()
	log.Printf("ConfigSync: Successfully synced multi_config.json to config.json")
	return nil
}

// BiDirectionalSync performs bidirectional synchronization based on file modification times
func (s *ConfigSyncService) BiDirectionalSync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configStat, err := os.Stat(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to stat config.json: %w", err)
	}

	multiConfigStat, err := os.Stat(s.multiConfigPath)
	if err != nil {
		return fmt.Errorf("failed to stat multi_config.json: %w", err)
	}

	// Determine which file was modified more recently
	if configStat.ModTime().After(multiConfigStat.ModTime()) {
		log.Printf("ConfigSync: config.json is newer, syncing to multi_config.json")
		return s.syncConfigToMultiInternal()
	} else if multiConfigStat.ModTime().After(configStat.ModTime()) {
		log.Printf("ConfigSync: multi_config.json is newer, syncing to config.json")
		return s.syncMultiToConfigInternal()
	}

	log.Printf("ConfigSync: Configurations are in sync")
	return nil
}

// GetSyncStatus returns the current synchronization status
func (s *ConfigSyncService) GetSyncStatus(ctx context.Context) (*ConfigSyncStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := &ConfigSyncStatus{
		LastSync:          s.lastSync,
		ConfigExists:      s.fileExists(s.configPath),
		MultiConfigExists: s.fileExists(s.multiConfigPath),
		AutoSyncEnabled:   s.autoSync,
	}

	// Check if files are in sync
	if status.ConfigExists && status.MultiConfigExists {
		configStat, _ := os.Stat(s.configPath)
		multiConfigStat, _ := os.Stat(s.multiConfigPath)
		
		status.InSync = abs(configStat.ModTime().Sub(multiConfigStat.ModTime())) < time.Second*10
		status.ConfigModTime = configStat.ModTime()
		status.MultiConfigModTime = multiConfigStat.ModTime()
	}

	return status, nil
}

// ValidateConfigurations validates both configuration formats for consistency
func (s *ConfigSyncService) ValidateConfigurations(ctx context.Context) (*ConfigValidationResult, error) {
	result := &ConfigValidationResult{
		Valid:  true,
		Errors: []string{},
	}

	// Validate traditional config
	if s.fileExists(s.configPath) {
		config, err := LoadConfig(s.configPath)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Traditional config error: %v", err))
		} else {
			configService := NewConfigService(s.configPath)
			if err := configService.ValidateConfig(config); err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Traditional config validation error: %v", err))
			}
		}
	}

	// Validate multi config
	if s.fileExists(s.multiConfigPath) {
		multiConfig, err := LoadMultiConfig(s.multiConfigPath)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Multi config error: %v", err))
		} else {
			if err := s.validateMultiConfig(multiConfig); err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Multi config validation error: %v", err))
			}
		}
	}

	return result, nil
}

// Internal helper methods

func (s *ConfigSyncService) syncConfigToMultiInternal() error {
	config, err := LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	multiConfig := MigrateFromLegacyConfig(config)
	if _, err := os.Stat(s.multiConfigPath); err == nil {
		existingMultiConfig, err := LoadMultiConfig(s.multiConfigPath)
		if err == nil {
			multiConfig = s.mergeConfigs(multiConfig, existingMultiConfig)
		}
	}

	return SaveMultiConfig(s.multiConfigPath, multiConfig)
}

func (s *ConfigSyncService) syncMultiToConfigInternal() error {
	multiConfig, err := LoadMultiConfig(s.multiConfigPath)
	if err != nil {
		return err
	}

	config, err := s.convertMultiToLegacy(multiConfig)
	if err != nil {
		return err
	}

	return SaveConfig(s.configPath, config)
}

func (s *ConfigSyncService) convertMultiToLegacy(multiConfig *MultiConfig) (*Config, error) {
	if len(multiConfig.DataSources) == 0 {
		return nil, fmt.Errorf("no data sources found in multi config")
	}

	if len(multiConfig.Sinks) == 0 {
		return nil, fmt.Errorf("no sinks found in multi config")
	}

	// Use the first MySQL data source
	var mysqlSource *DataSourceConfig
	for _, ds := range multiConfig.DataSources {
		if ds.Type == "mysql" {
			mysqlSource = &ds
			break
		}
	}
	if mysqlSource == nil {
		return nil, fmt.Errorf("no MySQL data source found in multi config")
	}

	// Use the first Elasticsearch sink
	var esSink *SinkConfig
	for _, sink := range multiConfig.Sinks {
		if sink.Type == "elasticsearch" {
			esSink = &sink
			break
		}
	}
	if esSink == nil {
		return nil, fmt.Errorf("no Elasticsearch sink found in multi config")
	}

	config := &Config{
		DBHost:       mysqlSource.Host,
		DBPort:       mysqlSource.Port,
		DBUser:       mysqlSource.User,
		DBPassword:   mysqlSource.Password,
		DBName:       mysqlSource.Database,
		ServerID:     mysqlSource.ServerID,
		TableFilters: mysqlSource.TableFilters,
		ESAddresses:  esSink.Addresses,
		ESUser:       esSink.User,
		ESPassword:   esSink.Password,
	}

	return config, nil
}

func (s *ConfigSyncService) mergeConfigs(newConfig, existingConfig *MultiConfig) *MultiConfig {
	// Start with the new config
	merged := *newConfig

	// Preserve additional data sources from existing config (ones that aren't "default-mysql")
	for _, existingDS := range existingConfig.DataSources {
		if existingDS.ID != "default-mysql" {
			found := false
			for _, newDS := range merged.DataSources {
				if newDS.ID == existingDS.ID {
					found = true
					break
				}
			}
			if !found {
				merged.DataSources = append(merged.DataSources, existingDS)
			}
		}
	}

	// Preserve additional sinks from existing config (ones that aren't "default-es")
	for _, existingSink := range existingConfig.Sinks {
		if existingSink.ID != "default-es" {
			found := false
			for _, newSink := range merged.Sinks {
				if newSink.ID == existingSink.ID {
					found = true
					break
				}
			}
			if !found {
				merged.Sinks = append(merged.Sinks, existingSink)
			}
		}
	}

	// Preserve additional jobs from existing config (ones that aren't "default-job")
	for _, existingJob := range existingConfig.Jobs {
		if existingJob.ID != "default-job" {
			found := false
			for _, newJob := range merged.Jobs {
				if newJob.ID == existingJob.ID {
					found = true
					break
				}
			}
			if !found {
				merged.Jobs = append(merged.Jobs, existingJob)
			}
		}
	}

	return &merged
}

func (s *ConfigSyncService) validateMultiConfig(config *MultiConfig) error {
	if config.Version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	// Validate data sources
	for _, ds := range config.DataSources {
		if ds.ID == "" {
			return fmt.Errorf("data source ID cannot be empty")
		}
		if ds.Host == "" {
			return fmt.Errorf("data source host cannot be empty for %s", ds.ID)
		}
		if ds.Port <= 0 || ds.Port > 65535 {
			return fmt.Errorf("data source port must be between 1 and 65535 for %s", ds.ID)
		}
	}

	// Validate sinks
	for _, sink := range config.Sinks {
		if sink.ID == "" {
			return fmt.Errorf("sink ID cannot be empty")
		}
		if len(sink.Addresses) == 0 {
			return fmt.Errorf("sink addresses cannot be empty for %s", sink.ID)
		}
	}

	// Validate jobs
	sourceIDs := make(map[string]bool)
	for _, ds := range config.DataSources {
		sourceIDs[ds.ID] = true
	}
	
	sinkIDs := make(map[string]bool)
	for _, sink := range config.Sinks {
		sinkIDs[sink.ID] = true
	}

	for _, job := range config.Jobs {
		if job.ID == "" {
			return fmt.Errorf("job ID cannot be empty")
		}
		if !sourceIDs[job.SourceID] {
			return fmt.Errorf("job %s references unknown data source %s", job.ID, job.SourceID)
		}
		if !sinkIDs[job.SinkID] {
			return fmt.Errorf("job %s references unknown sink %s", job.ID, job.SinkID)
		}
	}

	return nil
}

func (s *ConfigSyncService) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// ConfigSyncStatus represents the synchronization status
type ConfigSyncStatus struct {
	LastSync            time.Time `json:"last_sync"`
	ConfigExists        bool      `json:"config_exists"`
	MultiConfigExists   bool      `json:"multi_config_exists"`
	InSync              bool      `json:"in_sync"`
	AutoSyncEnabled     bool      `json:"auto_sync_enabled"`
	ConfigModTime       time.Time `json:"config_mod_time"`
	MultiConfigModTime  time.Time `json:"multi_config_mod_time"`
}

// ConfigValidationResult represents validation results
type ConfigValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// ConfigSyncOptions represents sync operation options
type ConfigSyncOptions struct {
	Direction    string `json:"direction"`    // "config_to_multi", "multi_to_config", "bidirectional"
	ForceSync    bool   `json:"force_sync"`   // ignore modification times
	BackupFirst  bool   `json:"backup_first"` // create backup before sync
	ValidateOnly bool   `json:"validate_only"` // only validate, don't sync
}

// CreateConfigBackup creates a backup of configuration files
func (s *ConfigSyncService) CreateConfigBackup(ctx context.Context) error {
	timestamp := time.Now().Format("20060102_150405")
	
	if s.fileExists(s.configPath) {
		backupPath := fmt.Sprintf("%s.backup_%s", s.configPath, timestamp)
		if err := s.copyFile(s.configPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup config.json: %w", err)
		}
		log.Printf("ConfigSync: Created backup %s", backupPath)
	}

	if s.fileExists(s.multiConfigPath) {
		backupPath := fmt.Sprintf("%s.backup_%s", s.multiConfigPath, timestamp)
		if err := s.copyFile(s.multiConfigPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup multi_config.json: %w", err)
		}
		log.Printf("ConfigSync: Created backup %s", backupPath)
	}

	return nil
}

func (s *ConfigSyncService) copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
