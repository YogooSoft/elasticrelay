package postgresql

import (
	"context"
	"testing"
	"time"

	"github.com/yogoosoft/elasticrelay/internal/config"
)

func TestNewConnector(t *testing.T) {
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     5432,
		DBUser:     "test_user",
		DBPassword: "test_password",
		DBName:     "test_db",
		TableFilters: []string{"table1", "table2"},
	}

	connector, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}

	if connector == nil {
		t.Fatal("Connector is nil")
	}

	if len(connector.tableFilters) != 2 {
		t.Errorf("Expected 2 table filters, got %d", len(connector.tableFilters))
	}

	if connector.dbName != "test_db" {
		t.Errorf("Expected database name 'test_db', got '%s'", connector.dbName)
	}

	if connector.slotName == "" {
		t.Error("Slot name should not be empty")
	}

	if connector.publication != "elasticrelay_publication" {
		t.Errorf("Expected publication 'elasticrelay_publication', got '%s'", connector.publication)
	}
}

func TestNewServer(t *testing.T) {
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     5432,
		DBUser:     "test_user",
		DBPassword: "test_password",
		DBName:     "test_db",
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server == nil {
		t.Fatal("Server is nil")
	}

	if server.config != cfg {
		t.Error("Server config does not match input config")
	}
}

func TestConnectorBuildTableList(t *testing.T) {
	connector := &Connector{
		tableFilters: []string{"users", "orders", "products"},
	}

	tableList := connector.buildTableList()
	expected := "users, orders, products"
	
	if tableList != expected {
		t.Errorf("Expected '%s', got '%s'", expected, tableList)
	}

	// Test empty filters
	connector.tableFilters = []string{}
	tableList = connector.buildTableList()
	if tableList != "" {
		t.Errorf("Expected empty string for no filters, got '%s'", tableList)
	}
}

func TestConnectorBuildLegacyConfig(t *testing.T) {
	// This test is more complex as it requires a properly initialized connector
	// For now, we'll test the basic structure
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     5432,
		DBUser:     "test_user",
		DBPassword: "test_password",
		DBName:     "test_db",
		TableFilters: []string{"table1"},
	}

	connector, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}

	legacyConfig := connector.buildLegacyConfig()
	
	if legacyConfig.DBHost != cfg.DBHost {
		t.Errorf("Expected host '%s', got '%s'", cfg.DBHost, legacyConfig.DBHost)
	}
	
	if legacyConfig.DBPort != cfg.DBPort {
		t.Errorf("Expected port %d, got %d", cfg.DBPort, legacyConfig.DBPort)
	}
}
