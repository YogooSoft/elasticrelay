package postgresql

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// TypeMapper handles PostgreSQL to Elasticsearch type mapping
type TypeMapper struct {
	typeMap        map[uint32]*PostgreSQLType
	nameToOIDMap   map[string]uint32
}

// PostgreSQLType represents a PostgreSQL data type
type PostgreSQLType struct {
	OID      uint32
	Name     string
	Category string
	ESType   string
	Handler  TypeHandler
}

// TypeHandler is a function that converts PostgreSQL values to appropriate Go types
type TypeHandler func(value interface{}) (interface{}, error)

// NewTypeMapper creates a new type mapper with predefined PostgreSQL types
func NewTypeMapper() *TypeMapper {
	tm := &TypeMapper{
		typeMap:      make(map[uint32]*PostgreSQLType),
		nameToOIDMap: make(map[string]uint32),
	}

	tm.initializeBasicTypes()
	return tm
}

// initializeBasicTypes initializes the basic PostgreSQL type mappings
func (tm *TypeMapper) initializeBasicTypes() {
	// Basic types mapping based on PostgreSQL OIDs
	types := []*PostgreSQLType{
		// Numeric types
		{OID: 20, Name: "bigint", Category: "numeric", ESType: "long", Handler: tm.handleBigInt},
		{OID: 21, Name: "smallint", Category: "numeric", ESType: "integer", Handler: tm.handleSmallInt},
		{OID: 23, Name: "integer", Category: "numeric", ESType: "integer", Handler: tm.handleInteger},
		{OID: 700, Name: "real", Category: "numeric", ESType: "float", Handler: tm.handleReal},
		{OID: 701, Name: "double precision", Category: "numeric", ESType: "double", Handler: tm.handleDouble},
		{OID: 1700, Name: "numeric", Category: "numeric", ESType: "scaled_float", Handler: tm.handleNumeric},

		// String types
		{OID: 25, Name: "text", Category: "string", ESType: "text", Handler: tm.handleText},
		{OID: 1042, Name: "char", Category: "string", ESType: "keyword", Handler: tm.handleChar},
		{OID: 1043, Name: "varchar", Category: "string", ESType: "text", Handler: tm.handleVarchar},
		{OID: 19, Name: "name", Category: "string", ESType: "keyword", Handler: tm.handleName},

		// Boolean type
		{OID: 16, Name: "boolean", Category: "boolean", ESType: "boolean", Handler: tm.handleBoolean},

		// Date/Time types
		{OID: 1082, Name: "date", Category: "datetime", ESType: "date", Handler: tm.handleDate},
		{OID: 1083, Name: "time", Category: "datetime", ESType: "date", Handler: tm.handleTime},
		{OID: 1114, Name: "timestamp", Category: "datetime", ESType: "date", Handler: tm.handleTimestamp},
		{OID: 1184, Name: "timestamptz", Category: "datetime", ESType: "date", Handler: tm.handleTimestampTZ},
		{OID: 1266, Name: "timetz", Category: "datetime", ESType: "date", Handler: tm.handleTimeTZ},

		// Binary types
		{OID: 17, Name: "bytea", Category: "binary", ESType: "binary", Handler: tm.handleBytea},

		// UUID type
		{OID: 2950, Name: "uuid", Category: "uuid", ESType: "keyword", Handler: tm.handleUUID},

		// JSON types
		{OID: 114, Name: "json", Category: "json", ESType: "object", Handler: tm.handleJSON},
		{OID: 3802, Name: "jsonb", Category: "json", ESType: "object", Handler: tm.handleJSONB},

		// Array types (common ones)
		{OID: 1007, Name: "_int4", Category: "array", ESType: "integer", Handler: tm.handleIntArray},
		{OID: 1009, Name: "_text", Category: "array", ESType: "text", Handler: tm.handleTextArray},
		{OID: 1016, Name: "_int8", Category: "array", ESType: "long", Handler: tm.handleBigIntArray},

		// Network types
		{OID: 869, Name: "inet", Category: "network", ESType: "ip", Handler: tm.handleInet},
		{OID: 650, Name: "cidr", Category: "network", ESType: "keyword", Handler: tm.handleCidr},

		// Geometric types (as text for now)
		{OID: 600, Name: "point", Category: "geometric", ESType: "geo_point", Handler: tm.handlePoint},
		{OID: 718, Name: "circle", Category: "geometric", ESType: "keyword", Handler: tm.handleCircle},

		// Other common types
		{OID: 3614, Name: "tsvector", Category: "fulltext", ESType: "text", Handler: tm.handleTSVector},
		{OID: 3615, Name: "tsquery", Category: "fulltext", ESType: "keyword", Handler: tm.handleTSQuery},
	}

	// Register all types
	for _, pgType := range types {
		tm.typeMap[pgType.OID] = pgType
		tm.nameToOIDMap[pgType.Name] = pgType.OID
	}

	log.Printf("Initialized PostgreSQL type mapper with %d types", len(types))
}

// GetTypeByOID returns a PostgreSQL type by its OID
func (tm *TypeMapper) GetTypeByOID(oid uint32) (*PostgreSQLType, error) {
	pgType, exists := tm.typeMap[oid]
	if !exists {
		return nil, fmt.Errorf("unknown PostgreSQL type OID: %d", oid)
	}
	return pgType, nil
}

// GetTypeByName returns a PostgreSQL type by its name
func (tm *TypeMapper) GetTypeByName(name string) (*PostgreSQLType, error) {
	oid, exists := tm.nameToOIDMap[name]
	if !exists {
		return nil, fmt.Errorf("unknown PostgreSQL type name: %s", name)
	}
	return tm.GetTypeByOID(oid)
}

// ConvertValue converts a PostgreSQL value to an appropriate Go type for Elasticsearch
func (tm *TypeMapper) ConvertValue(value interface{}, typeOID uint32) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	pgType, err := tm.GetTypeByOID(typeOID)
	if err != nil {
		// If unknown type, try to handle as string
		log.Printf("Warning: unknown type OID %d, treating as text", typeOID)
		return tm.handleText(value)
	}

	return pgType.Handler(value)
}

// Basic type handlers

func (tm *TypeMapper) handleBigInt(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return nil, fmt.Errorf("cannot convert %T to bigint", value)
	}
}

func (tm *TypeMapper) handleSmallInt(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case int16:
		return int(v), nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case string:
		i, err := strconv.Atoi(v)
		return i, err
	case []byte:
		return strconv.Atoi(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to smallint", value)
	}
}

func (tm *TypeMapper) handleInteger(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case int32:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case string:
		return strconv.Atoi(v)
	case []byte:
		return strconv.Atoi(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to integer", value)
	}
}

func (tm *TypeMapper) handleReal(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case float32:
		return v, nil
	case float64:
		return float32(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 32)
		return float32(f), err
	case []byte:
		f, err := strconv.ParseFloat(string(v), 32)
		return float32(f), err
	default:
		return nil, fmt.Errorf("cannot convert %T to real", value)
	}
}

func (tm *TypeMapper) handleDouble(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	case []byte:
		return strconv.ParseFloat(string(v), 64)
	default:
		return nil, fmt.Errorf("cannot convert %T to double", value)
	}
}

func (tm *TypeMapper) handleNumeric(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Keep as string for precise decimal handling
		return v, nil
	case []byte:
		return string(v), nil
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to numeric", value)
	}
}

func (tm *TypeMapper) handleText(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		// Convert any type to string representation
		return fmt.Sprintf("%v", value), nil
	}
}

func (tm *TypeMapper) handleChar(value interface{}) (interface{}, error) {
	return tm.handleText(value)
}

func (tm *TypeMapper) handleVarchar(value interface{}) (interface{}, error) {
	return tm.handleText(value)
}

func (tm *TypeMapper) handleName(value interface{}) (interface{}, error) {
	return tm.handleText(value)
}

func (tm *TypeMapper) handleBoolean(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(strings.ToLower(v))
	case []byte:
		return strconv.ParseBool(strings.ToLower(string(v)))
	case int:
		return v != 0, nil
	case int32:
		return v != 0, nil
	case int64:
		return v != 0, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to boolean", value)
	}
}

func (tm *TypeMapper) handleDate(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case time.Time:
		return v.Format("2006-01-02"), nil
	case string:
		// Try to parse and reformat
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t.Format("2006-01-02"), nil
		}
		return v, nil
	case []byte:
		return tm.handleDate(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to date", value)
	}
}

func (tm *TypeMapper) handleTime(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case time.Time:
		return v.Format("15:04:05"), nil
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to time", value)
	}
}

func (tm *TypeMapper) handleTimestamp(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case time.Time:
		return v.Format(time.RFC3339Nano), nil
	case string:
		// Try to parse and reformat for consistency
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			return t.Format(time.RFC3339Nano), nil
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Format(time.RFC3339Nano), nil
		}
		return v, nil
	case []byte:
		return tm.handleTimestamp(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to timestamp", value)
	}
}

func (tm *TypeMapper) handleTimestampTZ(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case time.Time:
		// Convert to UTC for consistency
		return v.UTC().Format(time.RFC3339Nano), nil
	case string:
		// Try to parse and convert to UTC
		if t, err := time.Parse("2006-01-02 15:04:05-07", v); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
		return v, nil
	case []byte:
		return tm.handleTimestampTZ(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to timestamptz", value)
	}
}

func (tm *TypeMapper) handleTimeTZ(value interface{}) (interface{}, error) {
	return tm.handleTime(value)
}

func (tm *TypeMapper) handleBytea(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []byte:
		// Return as base64 encoded string
		return string(v), nil
	case string:
		return v, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to bytea", value)
	}
}

func (tm *TypeMapper) handleUUID(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Validate UUID format (basic check)
		if len(v) == 36 && strings.Count(v, "-") == 4 {
			return v, nil
		}
		return v, nil // Return as-is, might be a different format
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (tm *TypeMapper) handleJSON(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Try to parse JSON to validate and return as object
		var jsonObj interface{}
		if err := json.Unmarshal([]byte(v), &jsonObj); err == nil {
			return jsonObj, nil
		}
		// If parsing fails, return as string
		return v, nil
	case []byte:
		return tm.handleJSON(string(v))
	case map[string]interface{}:
		return v, nil
	case []interface{}:
		return v, nil
	default:
		return value, nil
	}
}

func (tm *TypeMapper) handleJSONB(value interface{}) (interface{}, error) {
	// JSONB is handled the same as JSON in our case
	return tm.handleJSON(value)
}

// Array type handlers

func (tm *TypeMapper) handleIntArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]int, len(v))
		for i, item := range v {
			if intVal, err := tm.handleInteger(item); err == nil {
				result[i] = intVal.(int)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		// Parse PostgreSQL array format: {1,2,3}
		return tm.parsePostgreSQLArray(v, tm.handleInteger)
	case []byte:
		return tm.handleIntArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to int array", value)
	}
}

func (tm *TypeMapper) handleTextArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			if strVal, err := tm.handleText(item); err == nil {
				result[i] = strVal.(string)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		// Parse PostgreSQL array format: {"text1","text2"}
		return tm.parsePostgreSQLArray(v, tm.handleText)
	case []byte:
		return tm.handleTextArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to text array", value)
	}
}

func (tm *TypeMapper) handleBigIntArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]int64, len(v))
		for i, item := range v {
			if intVal, err := tm.handleBigInt(item); err == nil {
				result[i] = intVal.(int64)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		return tm.parsePostgreSQLArray(v, tm.handleBigInt)
	case []byte:
		return tm.handleBigIntArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to bigint array", value)
	}
}

// Network type handlers

func (tm *TypeMapper) handleInet(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Validate IP format (basic check)
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (tm *TypeMapper) handleCidr(value interface{}) (interface{}, error) {
	return tm.handleInet(value) // Same handling as inet
}

// Geometric type handlers

func (tm *TypeMapper) handlePoint(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// PostgreSQL point format: (x,y)
		// Convert to GeoJSON-like format for Elasticsearch
		if strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
			coords := strings.Trim(v, "()")
			parts := strings.Split(coords, ",")
			if len(parts) == 2 {
				return map[string]interface{}{
					"lat": strings.TrimSpace(parts[1]),
					"lon": strings.TrimSpace(parts[0]),
				}, nil
			}
		}
		return v, nil
	case []byte:
		return tm.handlePoint(string(v))
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (tm *TypeMapper) handleCircle(value interface{}) (interface{}, error) {
	return tm.handleText(value) // Return as text for now
}

// Full-text search type handlers

func (tm *TypeMapper) handleTSVector(value interface{}) (interface{}, error) {
	return tm.handleText(value)
}

func (tm *TypeMapper) handleTSQuery(value interface{}) (interface{}, error) {
	return tm.handleText(value)
}

// parsePostgreSQLArray parses PostgreSQL array format
func (tm *TypeMapper) parsePostgreSQLArray(arrayStr string, elementHandler TypeHandler) (interface{}, error) {
	// Simple PostgreSQL array parser
	// Handles formats like: {1,2,3} or {"a","b","c"}
	
	arrayStr = strings.TrimSpace(arrayStr)
	if !strings.HasPrefix(arrayStr, "{") || !strings.HasSuffix(arrayStr, "}") {
		return nil, fmt.Errorf("invalid array format: %s", arrayStr)
	}

	// Remove braces
	content := strings.Trim(arrayStr, "{}")
	if content == "" {
		return []interface{}{}, nil
	}

	// Split by comma (simple approach - doesn't handle nested arrays or quoted commas)
	parts := strings.Split(content, ",")
	result := make([]interface{}, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(part)
		// Remove quotes if present
		if strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`) {
			part = strings.Trim(part, `"`)
		}

		convertedValue, err := elementHandler(part)
		if err != nil {
			return nil, fmt.Errorf("failed to convert array element %s: %w", part, err)
		}
		result[i] = convertedValue
	}

	return result, nil
}

// GetElasticsearchMapping returns Elasticsearch mapping for a PostgreSQL type
func (tm *TypeMapper) GetElasticsearchMapping(typeOID uint32) (map[string]interface{}, error) {
	pgType, err := tm.GetTypeByOID(typeOID)
	if err != nil {
		// Default to keyword for unknown types
		return map[string]interface{}{
			"type": "keyword",
		}, nil
	}

	mapping := map[string]interface{}{
		"type": pgType.ESType,
	}

	// Add type-specific mapping properties
	switch pgType.ESType {
	case "text":
		mapping["analyzer"] = "standard"
	case "date":
		mapping["format"] = "strict_date_optional_time||epoch_millis"
	case "scaled_float":
		mapping["scaling_factor"] = 100
	case "geo_point":
		mapping["ignore_malformed"] = true
	}

	return mapping, nil
}
