package postgresql

import (
	"testing"
	"time"
)

func TestNewTypeMapper(t *testing.T) {
	tm := NewTypeMapper()

	if tm == nil {
		t.Fatal("TypeMapper is nil")
	}

	if len(tm.typeMap) == 0 {
		t.Error("TypeMapper should have predefined types")
	}

	if len(tm.nameToOIDMap) == 0 {
		t.Error("TypeMapper should have name to OID mappings")
	}
}

func TestGetTypeByOID(t *testing.T) {
	tm := NewTypeMapper()

	// Test known type
	pgType, err := tm.GetTypeByOID(25) // text type
	if err != nil {
		t.Fatalf("Failed to get text type: %v", err)
	}

	if pgType.Name != "text" {
		t.Errorf("Expected type name 'text', got '%s'", pgType.Name)
	}

	if pgType.ESType != "text" {
		t.Errorf("Expected ES type 'text', got '%s'", pgType.ESType)
	}

	// Test unknown type
	_, err = tm.GetTypeByOID(99999)
	if err == nil {
		t.Error("Expected error for unknown type OID")
	}
}

func TestGetTypeByName(t *testing.T) {
	tm := NewTypeMapper()

	// Test known type
	pgType, err := tm.GetTypeByName("boolean")
	if err != nil {
		t.Fatalf("Failed to get boolean type: %v", err)
	}

	if pgType.OID != 16 {
		t.Errorf("Expected OID 16 for boolean, got %d", pgType.OID)
	}

	// Test unknown type
	_, err = tm.GetTypeByName("unknown_type")
	if err == nil {
		t.Error("Expected error for unknown type name")
	}
}

func TestHandleBasicTypes(t *testing.T) {
	tm := NewTypeMapper()

	tests := []struct {
		name     string
		value    interface{}
		expected interface{}
		hasError bool
	}{
		{"text from string", "hello", "hello", false},
		{"text from bytes", []byte("hello"), "hello", false},
		{"boolean true", true, true, false},
		{"boolean from string", "true", true, false},
		{"boolean from string false", "false", false, false},
		{"integer from int32", int32(42), 42, false},
		{"integer from string", "42", 42, false},
		{"bigint from int64", int64(123456789), int64(123456789), false},
		{"bigint from string", "123456789", int64(123456789), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result interface{}
			var err error

			switch tt.name {
			case "text from string", "text from bytes":
				result, err = tm.handleText(tt.value)
			case "boolean true", "boolean from string", "boolean from string false":
				result, err = tm.handleBoolean(tt.value)
			case "integer from int32", "integer from string":
				result, err = tm.handleInteger(tt.value)
			case "bigint from int64", "bigint from string":
				result, err = tm.handleBigInt(tt.value)
			}

			if tt.hasError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHandleJSON(t *testing.T) {
	tm := NewTypeMapper()

	// Test valid JSON string
	jsonStr := `{"name": "test", "value": 42}`
	result, err := tm.handleJSON(jsonStr)
	if err != nil {
		t.Fatalf("Failed to handle JSON: %v", err)
	}

	// Should be parsed as object
	jsonObj, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", result)
	}

	if jsonObj["name"] != "test" {
		t.Errorf("Expected name 'test', got %v", jsonObj["name"])
	}

	// Test invalid JSON - should return as string
	invalidJSON := `{"invalid": json`
	result, err = tm.handleJSON(invalidJSON)
	if err != nil {
		t.Fatalf("Failed to handle invalid JSON: %v", err)
	}

	if result != invalidJSON {
		t.Errorf("Expected invalid JSON to be returned as string")
	}
}

func TestHandleTimestamp(t *testing.T) {
	tm := NewTypeMapper()

	// Test time.Time
	now := time.Now()
	result, err := tm.handleTimestamp(now)
	if err != nil {
		t.Fatalf("Failed to handle timestamp: %v", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("Expected string result, got %T", result)
	}

	// Parse back to verify format
	_, err = time.Parse(time.RFC3339Nano, resultStr)
	if err != nil {
		t.Errorf("Result is not valid RFC3339Nano format: %v", err)
	}

	// Test string input
	timeStr := "2023-01-01 12:00:00"
	result, err = tm.handleTimestamp(timeStr)
	if err != nil {
		t.Fatalf("Failed to handle timestamp string: %v", err)
	}

	// Should be a valid time string
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

func TestConvertValue(t *testing.T) {
	tm := NewTypeMapper()

	// Test nil value
	result, err := tm.ConvertValue(nil, 25)
	if err != nil {
		t.Errorf("Failed to convert nil value: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result for nil input")
	}

	// Test text conversion
	result, err = tm.ConvertValue("hello", 25) // text type OID
	if err != nil {
		t.Errorf("Failed to convert text value: %v", err)
	}
	if result != "hello" {
		t.Errorf("Expected 'hello', got %v", result)
	}

	// Test boolean conversion
	result, err = tm.ConvertValue(true, 16) // boolean type OID
	if err != nil {
		t.Errorf("Failed to convert boolean value: %v", err)
	}
	if result != true {
		t.Errorf("Expected true, got %v", result)
	}

	// Test unknown type - should fallback to text
	result, err = tm.ConvertValue("test", 99999)
	if err != nil {
		t.Errorf("Failed to convert unknown type: %v", err)
	}
	if result != "test" {
		t.Errorf("Expected 'test', got %v", result)
	}
}

func TestParsePostgreSQLArray(t *testing.T) {
	tm := NewTypeMapper()

	// Test integer array
	arrayStr := "{1,2,3,4,5}"
	result, err := tm.parsePostgreSQLArray(arrayStr, tm.handleInteger)
	if err != nil {
		t.Fatalf("Failed to parse array: %v", err)
	}

	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{}, got %T", result)
	}

	if len(resultSlice) != 5 {
		t.Errorf("Expected 5 elements, got %d", len(resultSlice))
	}

	if resultSlice[0] != 1 {
		t.Errorf("Expected first element to be 1, got %v", resultSlice[0])
	}

	// Test text array
	textArray := `{"hello","world","test"}`
	result, err = tm.parsePostgreSQLArray(textArray, tm.handleText)
	if err != nil {
		t.Fatalf("Failed to parse text array: %v", err)
	}

	resultSlice, ok = result.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{}, got %T", result)
	}

	if len(resultSlice) != 3 {
		t.Errorf("Expected 3 elements, got %d", len(resultSlice))
	}

	if resultSlice[0] != "hello" {
		t.Errorf("Expected first element to be 'hello', got %v", resultSlice[0])
	}

	// Test empty array
	emptyArray := "{}"
	result, err = tm.parsePostgreSQLArray(emptyArray, tm.handleText)
	if err != nil {
		t.Fatalf("Failed to parse empty array: %v", err)
	}

	resultSlice, ok = result.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{}, got %T", result)
	}

	if len(resultSlice) != 0 {
		t.Errorf("Expected 0 elements, got %d", len(resultSlice))
	}
}

func TestGetElasticsearchMapping(t *testing.T) {
	tm := NewTypeMapper()

	// Test text type mapping
	mapping, err := tm.GetElasticsearchMapping(25) // text type
	if err != nil {
		t.Fatalf("Failed to get ES mapping: %v", err)
	}

	if mapping["type"] != "text" {
		t.Errorf("Expected ES type 'text', got %v", mapping["type"])
	}

	if mapping["analyzer"] != "standard" {
		t.Errorf("Expected analyzer 'standard', got %v", mapping["analyzer"])
	}

	// Test date type mapping
	mapping, err = tm.GetElasticsearchMapping(1114) // timestamp type
	if err != nil {
		t.Fatalf("Failed to get ES mapping for timestamp: %v", err)
	}

	if mapping["type"] != "date" {
		t.Errorf("Expected ES type 'date', got %v", mapping["type"])
	}

	// Test unknown type - should default to keyword
	mapping, err = tm.GetElasticsearchMapping(99999)
	if err != nil {
		t.Fatalf("Failed to get ES mapping for unknown type: %v", err)
	}

	if mapping["type"] != "keyword" {
		t.Errorf("Expected default ES type 'keyword', got %v", mapping["type"])
	}
}
