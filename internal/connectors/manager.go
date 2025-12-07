package connectors

import (
	"fmt"
	"log"
	"sync"

	"github.com/yogoosoft/elasticrelay/internal/config"
	"github.com/yogoosoft/elasticrelay/internal/connectors/mongodb"
	"github.com/yogoosoft/elasticrelay/internal/connectors/mysql"
	"github.com/yogoosoft/elasticrelay/internal/connectors/postgresql"
)

// ConnectorManager manages multiple connector instances for different data sources
type ConnectorManager struct {
	connectors map[string]*ConnectorInstance
	mu         sync.RWMutex
}

// ConnectorInstance represents a single data source connector instance
type ConnectorInstance struct {
	ID         string
	Type       string // mysql, postgresql, mongodb
	Config     *config.DataSourceConfig
	Connector  interface{} // Actual connector (mysql.Connector, pg.Connector, etc.)
	ActiveJobs map[string]bool
	mu         sync.RWMutex
}

// NewConnectorManager creates a new connector manager
func NewConnectorManager() *ConnectorManager {
	return &ConnectorManager{
		connectors: make(map[string]*ConnectorInstance),
	}
}

// RegisterDataSource registers a new data source and creates its connector
func (cm *ConnectorManager) RegisterDataSource(dataSource *config.DataSourceConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if already exists
	if _, exists := cm.connectors[dataSource.ID]; exists {
		return fmt.Errorf("data source %s already registered", dataSource.ID)
	}

	// Create connector instance based on type
	var connector interface{}
	var err error

	switch dataSource.Type {
	case "mysql":
		connector, err = cm.createMySQLConnector(dataSource)
	case "postgresql":
		connector, err = cm.createPostgreSQLConnector(dataSource)
	case "mongodb":
		connector, err = cm.createMongoDBConnector(dataSource)
	default:
		return fmt.Errorf("unsupported data source type: %s", dataSource.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create %s connector: %w", dataSource.Type, err)
	}

	instance := &ConnectorInstance{
		ID:         dataSource.ID,
		Type:       dataSource.Type,
		Config:     dataSource,
		Connector:  connector,
		ActiveJobs: make(map[string]bool),
	}

	cm.connectors[dataSource.ID] = instance

	log.Printf("ConnectorManager: Registered %s data source '%s' (%s:%d)",
		dataSource.Type, dataSource.ID, dataSource.Host, dataSource.Port)

	return nil
}

// createMySQLConnector creates a MySQL connector instance
func (cm *ConnectorManager) createMySQLConnector(dataSource *config.DataSourceConfig) (*mysql.Connector, error) {
	// Convert multi-config to legacy config for MySQL connector
	legacyConfig := &config.Config{
		DBHost:       dataSource.Host,
		DBPort:       dataSource.Port,
		DBUser:       dataSource.User,
		DBPassword:   dataSource.Password,
		DBName:       dataSource.Database,
		ServerID:     dataSource.ServerID,
		TableFilters: dataSource.TableFilters,
	}

	return mysql.NewConnector(legacyConfig)
}

// createPostgreSQLConnector creates a PostgreSQL connector instance
func (cm *ConnectorManager) createPostgreSQLConnector(dataSource *config.DataSourceConfig) (*postgresql.Connector, error) {
	// Convert multi-config to legacy config for PostgreSQL connector
	legacyConfig := &config.Config{
		DBHost:       dataSource.Host,
		DBPort:       dataSource.Port,
		DBUser:       dataSource.User,
		DBPassword:   dataSource.Password,
		DBName:       dataSource.Database,
		TableFilters: dataSource.TableFilters,
	}

	return postgresql.NewConnector(legacyConfig)
}

// createMongoDBConnector creates a MongoDB connector instance
func (cm *ConnectorManager) createMongoDBConnector(dataSource *config.DataSourceConfig) (*mongodb.Connector, error) {
	// Convert multi-config to legacy config for MongoDB connector
	legacyConfig := &config.Config{
		DBHost:       dataSource.Host,
		DBPort:       dataSource.Port,
		DBUser:       dataSource.User,
		DBPassword:   dataSource.Password,
		DBName:       dataSource.Database,
		TableFilters: dataSource.TableFilters, // Used as collection filters
	}

	return mongodb.NewConnector(legacyConfig)
}

// UnregisterDataSource removes a data source connector
func (cm *ConnectorManager) UnregisterDataSource(dataSourceID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	instance, exists := cm.connectors[dataSourceID]
	if !exists {
		return fmt.Errorf("data source %s not found", dataSourceID)
	}

	// Check if there are active jobs
	instance.mu.RLock()
	activeJobCount := len(instance.ActiveJobs)
	instance.mu.RUnlock()

	if activeJobCount > 0 {
		return fmt.Errorf("cannot unregister data source %s: has %d active jobs",
			dataSourceID, activeJobCount)
	}

	delete(cm.connectors, dataSourceID)

	log.Printf("ConnectorManager: Unregistered data source '%s'", dataSourceID)
	return nil
}

// GetConnector returns the connector instance for a data source
func (cm *ConnectorManager) GetConnector(dataSourceID string) (*ConnectorInstance, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	instance, exists := cm.connectors[dataSourceID]
	if !exists {
		return nil, fmt.Errorf("data source %s not found", dataSourceID)
	}

	return instance, nil
}

// AddJobToDataSource adds a job to a data source's active job list
func (cm *ConnectorManager) AddJobToDataSource(dataSourceID, jobID string) error {
	instance, err := cm.GetConnector(dataSourceID)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.ActiveJobs[jobID] = true

	log.Printf("ConnectorManager: Added job '%s' to data source '%s'", jobID, dataSourceID)
	return nil
}

// RemoveJobFromDataSource removes a job from a data source's active job list
func (cm *ConnectorManager) RemoveJobFromDataSource(dataSourceID, jobID string) error {
	instance, err := cm.GetConnector(dataSourceID)
	if err != nil {
		return err
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	delete(instance.ActiveJobs, jobID)

	log.Printf("ConnectorManager: Removed job '%s' from data source '%s'", jobID, dataSourceID)
	return nil
}

// ListConnectors returns all registered connector instances
func (cm *ConnectorManager) ListConnectors() map[string]*ConnectorInstance {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]*ConnectorInstance)
	for id, instance := range cm.connectors {
		result[id] = instance
	}

	return result
}

// LoadFromMultiConfig loads all data sources from multi-config
func (cm *ConnectorManager) LoadFromMultiConfig(multiConfig *config.MultiConfig) error {
	for _, dataSource := range multiConfig.DataSources {
		if err := cm.RegisterDataSource(&dataSource); err != nil {
			log.Printf("ConnectorManager: Failed to register data source %s: %v",
				dataSource.ID, err)
			continue
		}
	}

	log.Printf("ConnectorManager: Loaded %d data sources from configuration",
		len(multiConfig.DataSources))

	return nil
}

// ValidateDataSources validates all data source configurations
func (cm *ConnectorManager) ValidateDataSources() []error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var errors []error

	for id, instance := range cm.connectors {
		if err := cm.validateDataSource(instance); err != nil {
			errors = append(errors, fmt.Errorf("data source %s: %w", id, err))
		}
	}

	return errors
}

// validateDataSource validates a single data source configuration
func (cm *ConnectorManager) validateDataSource(instance *ConnectorInstance) error {
	config := instance.Config

	if config.Host == "" {
		return fmt.Errorf("host cannot be empty")
	}
	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if config.User == "" {
		return fmt.Errorf("user cannot be empty")
	}
	if config.Database == "" {
		return fmt.Errorf("database cannot be empty")
	}

	// MySQL specific validation
	if config.Type == "mysql" && config.ServerID == 0 {
		return fmt.Errorf("MySQL ServerID cannot be 0")
	}

	// PostgreSQL specific validation
	if config.Type == "postgresql" {
		// Add any PostgreSQL-specific validation here
		// For now, basic validation is sufficient
	}

	return nil
}
