package config

import (
	"log"
	"sync"
)

// ConfigReloader provides hot reloading functionality for configuration.
type ConfigReloader struct {
	configPath string
	listeners  []func(*Config)
	mu         sync.RWMutex
}

// NewConfigReloader creates a new configuration reloader.
func NewConfigReloader(configPath string) *ConfigReloader {
	return &ConfigReloader{
		configPath: configPath,
		listeners:  make([]func(*Config), 0),
	}
}

// AddListener adds a listener that will be called when configuration is reloaded.
func (r *ConfigReloader) AddListener(listener func(*Config)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, listener)
}

// Reload reloads the configuration and notifies all listeners.
func (r *ConfigReloader) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset the once variable to allow reloading
	cfgMux.Lock()
	once = sync.Once{}
	cfg = nil
	cfgMux.Unlock()

	// Load the new configuration
	newConfig, err := LoadConfig(r.configPath)
	if err != nil {
		log.Printf("ConfigReloader: Failed to reload configuration: %v", err)
		return err
	}

	// Notify all listeners
	for _, listener := range r.listeners {
		go func(l func(*Config)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("ConfigReloader: Listener panic: %v", r)
				}
			}()
			l(newConfig)
		}(listener)
	}

	log.Printf("ConfigReloader: Configuration reloaded successfully")
	return nil
}

// HotUpdate updates the configuration in memory and on disk, then notifies listeners.
func (r *ConfigReloader) HotUpdate(newConfig *Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate the configuration
	service := NewConfigService(r.configPath)
	if err := service.ValidateConfig(newConfig); err != nil {
		return err
	}

	// Save to disk
	if err := UpdateConfig(newConfig); err != nil {
		log.Printf("ConfigReloader: Failed to update configuration: %v", err)
		return err
	}

	// Notify all listeners
	for _, listener := range r.listeners {
		go func(l func(*Config)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("ConfigReloader: Listener panic: %v", r)
				}
			}()
			l(newConfig)
		}(listener)
	}

	log.Printf("ConfigReloader: Configuration hot updated successfully")
	return nil
}

// GetConfig returns the current configuration.
func (r *ConfigReloader) GetConfig() *Config {
	return GetConfig()
}
