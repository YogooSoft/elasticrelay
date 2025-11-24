package postgresql

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdvancedTypeMapper extends the basic type mapper with support for advanced PostgreSQL types
type AdvancedTypeMapper struct {
	*TypeMapper
	pool               *pgxpool.Pool
	enumTypes          map[uint32]*EnumType
	compositeTypes     map[uint32]*CompositeType
	domainTypes        map[uint32]*DomainType
	arrayTypeCache     map[uint32]uint32 // array OID -> element OID
}

// EnumType represents a PostgreSQL ENUM type
type EnumType struct {
	OID    uint32
	Name   string
	Labels []string
}

// CompositeType represents a PostgreSQL composite type
type CompositeType struct {
	OID     uint32
	Name    string
	Columns []CompositeColumn
}

// CompositeColumn represents a column in a composite type
type CompositeColumn struct {
	Name   string
	TypeOID uint32
	TypeName string
}

// DomainType represents a PostgreSQL domain type
type DomainType struct {
	OID        uint32
	Name       string
	BaseTypeOID uint32
	BaseTypeName string
	NotNull    bool
	Default    *string
	Check      *string
}

// NewAdvancedTypeMapper creates a new advanced type mapper
func NewAdvancedTypeMapper(pool *pgxpool.Pool) *AdvancedTypeMapper {
	atm := &AdvancedTypeMapper{
		TypeMapper:     NewTypeMapper(),
		pool:           pool,
		enumTypes:      make(map[uint32]*EnumType),
		compositeTypes: make(map[uint32]*CompositeType),
		domainTypes:    make(map[uint32]*DomainType),
		arrayTypeCache: make(map[uint32]uint32),
	}

	atm.initializeAdvancedTypes()
	return atm
}

// initializeAdvancedTypes adds advanced type handlers
func (atm *AdvancedTypeMapper) initializeAdvancedTypes() {
	// Add more advanced types to the type mapper
	advancedTypes := []*PostgreSQLType{
		// Interval type
		{OID: 1186, Name: "interval", Category: "datetime", ESType: "keyword", Handler: atm.handleInterval},
		
		// Money type
		{OID: 790, Name: "money", Category: "numeric", ESType: "scaled_float", Handler: atm.handleMoney},
		
		// Bit types
		{OID: 1560, Name: "bit", Category: "bitstring", ESType: "keyword", Handler: atm.handleBit},
		{OID: 1562, Name: "varbit", Category: "bitstring", ESType: "keyword", Handler: atm.handleVarbit},
		
		// Range types
		{OID: 3904, Name: "int4range", Category: "range", ESType: "keyword", Handler: atm.handleRange},
		{OID: 3906, Name: "numrange", Category: "range", ESType: "keyword", Handler: atm.handleRange},
		{OID: 3908, Name: "tsrange", Category: "range", ESType: "keyword", Handler: atm.handleRange},
		{OID: 3910, Name: "tstzrange", Category: "range", ESType: "keyword", Handler: atm.handleRange},
		{OID: 3912, Name: "daterange", Category: "range", ESType: "keyword", Handler: atm.handleRange},
		{OID: 3926, Name: "int8range", Category: "range", ESType: "keyword", Handler: atm.handleRange},
		
		// XML type
		{OID: 142, Name: "xml", Category: "xml", ESType: "text", Handler: atm.handleXML},
		
		// HSTORE type
		{OID: 0, Name: "hstore", Category: "hstore", ESType: "object", Handler: atm.handleHStore}, // OID varies
		
		// More array types
		{OID: 1000, Name: "_bool", Category: "array", ESType: "boolean", Handler: atm.handleBooleanArray},
		{OID: 1021, Name: "_float4", Category: "array", ESType: "float", Handler: atm.handleFloatArray},
		{OID: 1022, Name: "_float8", Category: "array", ESType: "double", Handler: atm.handleDoubleArray},
		{OID: 2951, Name: "_uuid", Category: "array", ESType: "keyword", Handler: atm.handleUUIDArray},
		
		// Multirange types (PostgreSQL 14+)
		{OID: 4451, Name: "int4multirange", Category: "multirange", ESType: "keyword", Handler: atm.handleMultiRange},
		{OID: 4532, Name: "nummultirange", Category: "multirange", ESType: "keyword", Handler: atm.handleMultiRange},
	}

	// Register advanced types
	for _, pgType := range advancedTypes {
		if pgType.OID != 0 { // Skip types with dynamic OIDs
			atm.typeMap[pgType.OID] = pgType
			atm.nameToOIDMap[pgType.Name] = pgType.OID
		}
	}
}

// LoadCustomTypes loads custom types from the database
func (atm *AdvancedTypeMapper) LoadCustomTypes(ctx context.Context) error {
	log.Printf("Loading custom PostgreSQL types from database...")
	
	if err := atm.loadEnumTypes(ctx); err != nil {
		return fmt.Errorf("failed to load enum types: %w", err)
	}
	
	if err := atm.loadCompositeTypes(ctx); err != nil {
		return fmt.Errorf("failed to load composite types: %w", err)
	}
	
	if err := atm.loadDomainTypes(ctx); err != nil {
		return fmt.Errorf("failed to load domain types: %w", err)
	}
	
	if err := atm.loadArrayTypes(ctx); err != nil {
		return fmt.Errorf("failed to load array types: %w", err)
	}

	log.Printf("Loaded custom types: %d enums, %d composites, %d domains", 
		len(atm.enumTypes), len(atm.compositeTypes), len(atm.domainTypes))
	
	return nil
}

// loadEnumTypes loads ENUM types from pg_enum
func (atm *AdvancedTypeMapper) loadEnumTypes(ctx context.Context) error {
	query := `
		SELECT t.oid, t.typname, array_agg(e.enumlabel ORDER BY e.enumsortorder) as labels
		FROM pg_type t
		JOIN pg_enum e ON t.oid = e.enumtypid
		WHERE t.typtype = 'e'
		GROUP BY t.oid, t.typname`
	
	rows, err := atm.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query enum types: %w", err)
	}
	defer rows.Close()
	
	for rows.Next() {
		var oid uint32
		var name string
		var labels []string
		
		if err := rows.Scan(&oid, &name, &labels); err != nil {
			return fmt.Errorf("failed to scan enum type: %w", err)
		}
		
		enumType := &EnumType{
			OID:    oid,
			Name:   name,
			Labels: labels,
		}
		
		atm.enumTypes[oid] = enumType
		
		// Register as a type
		atm.typeMap[oid] = &PostgreSQLType{
			OID:      oid,
			Name:     name,
			Category: "enum",
			ESType:   "keyword",
			Handler:  atm.handleEnum,
		}
		atm.nameToOIDMap[name] = oid
	}
	
	return nil
}

// loadCompositeTypes loads composite types from pg_class and pg_attribute
func (atm *AdvancedTypeMapper) loadCompositeTypes(ctx context.Context) error {
	query := `
		SELECT 
			c.reltype as oid,
			c.relname as name,
			array_agg(a.attname ORDER BY a.attnum) as col_names,
			array_agg(a.atttypid ORDER BY a.attnum) as col_type_oids,
			array_agg(t.typname ORDER BY a.attnum) as col_type_names
		FROM pg_class c
		JOIN pg_attribute a ON c.oid = a.attrelid
		JOIN pg_type t ON a.atttypid = t.oid
		WHERE c.relkind = 'c' AND a.attnum > 0 AND NOT a.attisdropped
		GROUP BY c.reltype, c.relname`
	
	rows, err := atm.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query composite types: %w", err)
	}
	defer rows.Close()
	
	for rows.Next() {
		var oid uint32
		var name string
		var colNames []string
		var colTypeOIDs []uint32
		var colTypeNames []string
		
		if err := rows.Scan(&oid, &name, &colNames, &colTypeOIDs, &colTypeNames); err != nil {
			return fmt.Errorf("failed to scan composite type: %w", err)
		}
		
		columns := make([]CompositeColumn, len(colNames))
		for i := range colNames {
			columns[i] = CompositeColumn{
				Name:     colNames[i],
				TypeOID:  colTypeOIDs[i],
				TypeName: colTypeNames[i],
			}
		}
		
		compositeType := &CompositeType{
			OID:     oid,
			Name:    name,
			Columns: columns,
		}
		
		atm.compositeTypes[oid] = compositeType
		
		// Register as a type
		atm.typeMap[oid] = &PostgreSQLType{
			OID:      oid,
			Name:     name,
			Category: "composite",
			ESType:   "object",
			Handler:  atm.handleComposite,
		}
		atm.nameToOIDMap[name] = oid
	}
	
	return nil
}

// loadDomainTypes loads domain types from pg_type
func (atm *AdvancedTypeMapper) loadDomainTypes(ctx context.Context) error {
	query := `
		SELECT 
			t.oid,
			t.typname,
			t.typbasetype,
			bt.typname as base_type_name,
			t.typnotnull,
			t.typdefault,
			c.consrc
		FROM pg_type t
		JOIN pg_type bt ON t.typbasetype = bt.oid
		LEFT JOIN pg_constraint c ON t.oid = c.contypid AND c.contype = 'c'
		WHERE t.typtype = 'd'`
	
	rows, err := atm.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query domain types: %w", err)
	}
	defer rows.Close()
	
	for rows.Next() {
		var oid uint32
		var name string
		var baseTypeOID uint32
		var baseTypeName string
		var notNull bool
		var defaultVal *string
		var checkConstraint *string
		
		if err := rows.Scan(&oid, &name, &baseTypeOID, &baseTypeName, &notNull, &defaultVal, &checkConstraint); err != nil {
			return fmt.Errorf("failed to scan domain type: %w", err)
		}
		
		domainType := &DomainType{
			OID:          oid,
			Name:         name,
			BaseTypeOID:  baseTypeOID,
			BaseTypeName: baseTypeName,
			NotNull:      notNull,
			Default:      defaultVal,
			Check:        checkConstraint,
		}
		
		atm.domainTypes[oid] = domainType
		
		// Register as a type (use base type's ES type)
		baseType, _ := atm.GetTypeByOID(baseTypeOID)
		esType := "keyword"
		if baseType != nil {
			esType = baseType.ESType
		}
		
		atm.typeMap[oid] = &PostgreSQLType{
			OID:      oid,
			Name:     name,
			Category: "domain",
			ESType:   esType,
			Handler:  atm.handleDomain,
		}
		atm.nameToOIDMap[name] = oid
	}
	
	return nil
}

// loadArrayTypes loads array type mappings
func (atm *AdvancedTypeMapper) loadArrayTypes(ctx context.Context) error {
	query := `
		SELECT t.oid as array_oid, t.typelem as element_oid, t.typname, et.typname as element_name
		FROM pg_type t
		JOIN pg_type et ON t.typelem = et.oid
		WHERE t.typelem != 0`
	
	rows, err := atm.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query array types: %w", err)
	}
	defer rows.Close()
	
	for rows.Next() {
		var arrayOID, elementOID uint32
		var arrayName, elementName string
		
		if err := rows.Scan(&arrayOID, &elementOID, &arrayName, &elementName); err != nil {
			return fmt.Errorf("failed to scan array type: %w", err)
		}
		
		atm.arrayTypeCache[arrayOID] = elementOID
		
		// Register array type if not already registered
		if _, exists := atm.typeMap[arrayOID]; !exists {
			// Determine ES type based on element type
			elementType, _ := atm.GetTypeByOID(elementOID)
			esType := "keyword"
			if elementType != nil {
				esType = elementType.ESType
			}
			
			atm.typeMap[arrayOID] = &PostgreSQLType{
				OID:      arrayOID,
				Name:     arrayName,
				Category: "array",
				ESType:   esType,
				Handler:  atm.handleGenericArray,
			}
			atm.nameToOIDMap[arrayName] = arrayOID
		}
	}
	
	return nil
}

// Advanced type handlers

func (atm *AdvancedTypeMapper) handleEnum(value interface{}) (interface{}, error) {
	// Enum values are stored as strings
	return atm.handleText(value)
}

func (atm *AdvancedTypeMapper) handleComposite(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// PostgreSQL composite format: (value1,value2,...)
		return atm.parseCompositeValue(v)
	case []byte:
		return atm.handleComposite(string(v))
	case map[string]interface{}:
		return v, nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (atm *AdvancedTypeMapper) handleDomain(value interface{}) (interface{}, error) {
	// Domain types use their base type handler
	// For now, just return as text
	return atm.handleText(value)
}

func (atm *AdvancedTypeMapper) handleInterval(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// PostgreSQL interval format like "1 day 2 hours"
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (atm *AdvancedTypeMapper) handleMoney(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Remove currency symbols and parse as float
		cleaned := regexp.MustCompile(`[^\d.-]`).ReplaceAllString(v, "")
		if f, err := strconv.ParseFloat(cleaned, 64); err == nil {
			return f, nil
		}
		return v, nil
	case []byte:
		return atm.handleMoney(string(v))
	case float64:
		return v, nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (atm *AdvancedTypeMapper) handleBit(value interface{}) (interface{}, error) {
	return atm.handleText(value)
}

func (atm *AdvancedTypeMapper) handleVarbit(value interface{}) (interface{}, error) {
	return atm.handleText(value)
}

func (atm *AdvancedTypeMapper) handleRange(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// PostgreSQL range format: [1,10) or (1,10]
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (atm *AdvancedTypeMapper) handleMultiRange(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// PostgreSQL multirange format: {[1,3),[5,7)}
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

func (atm *AdvancedTypeMapper) handleXML(value interface{}) (interface{}, error) {
	return atm.handleText(value)
}

func (atm *AdvancedTypeMapper) handleHStore(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Parse hstore format: "key1"=>"value1", "key2"=>"value2"
		return atm.parseHStore(v)
	case []byte:
		return atm.handleHStore(string(v))
	case map[string]interface{}:
		return v, nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// Array handlers for advanced types

func (atm *AdvancedTypeMapper) handleBooleanArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]bool, len(v))
		for i, item := range v {
			if boolVal, err := atm.handleBoolean(item); err == nil {
				result[i] = boolVal.(bool)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		return atm.parsePostgreSQLArray(v, atm.handleBoolean)
	case []byte:
		return atm.handleBooleanArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to boolean array", value)
	}
}

func (atm *AdvancedTypeMapper) handleFloatArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]float32, len(v))
		for i, item := range v {
			if floatVal, err := atm.handleReal(item); err == nil {
				result[i] = floatVal.(float32)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		return atm.parsePostgreSQLArray(v, atm.handleReal)
	case []byte:
		return atm.handleFloatArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to float array", value)
	}
}

func (atm *AdvancedTypeMapper) handleDoubleArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]float64, len(v))
		for i, item := range v {
			if doubleVal, err := atm.handleDouble(item); err == nil {
				result[i] = doubleVal.(float64)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		return atm.parsePostgreSQLArray(v, atm.handleDouble)
	case []byte:
		return atm.handleDoubleArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to double array", value)
	}
}

func (atm *AdvancedTypeMapper) handleUUIDArray(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			if uuidVal, err := atm.handleUUID(item); err == nil {
				result[i] = uuidVal.(string)
			} else {
				return nil, fmt.Errorf("invalid array element at index %d: %w", i, err)
			}
		}
		return result, nil
	case string:
		return atm.parsePostgreSQLArray(v, atm.handleUUID)
	case []byte:
		return atm.handleUUIDArray(string(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to UUID array", value)
	}
}

func (atm *AdvancedTypeMapper) handleGenericArray(value interface{}) (interface{}, error) {
	// Generic array handler for custom types
	return atm.parsePostgreSQLArray(fmt.Sprintf("%v", value), atm.handleText)
}

// Utility functions

func (atm *AdvancedTypeMapper) parseCompositeValue(value string) (map[string]interface{}, error) {
	// Simple PostgreSQL composite parser
	// Format: (value1,value2,value3)
	
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "(") || !strings.HasSuffix(value, ")") {
		return nil, fmt.Errorf("invalid composite format: %s", value)
	}
	
	// This is a simplified parser - a full implementation would handle
	// quoted values, nested composites, etc.
	content := strings.Trim(value, "()")
	parts := strings.Split(content, ",")
	
	result := make(map[string]interface{})
	for i, part := range parts {
		key := fmt.Sprintf("field_%d", i+1)
		result[key] = strings.TrimSpace(part)
	}
	
	return result, nil
}

func (atm *AdvancedTypeMapper) parseHStore(value string) (map[string]interface{}, error) {
	// Simple hstore parser
	// Format: "key1"=>"value1", "key2"=>"value2"
	
	result := make(map[string]interface{})
	
	// This is a very basic parser - a production implementation would need
	// to handle escaped quotes, NULL values, etc.
	pairs := strings.Split(value, ",")
	
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), "=>")
		if len(parts) == 2 {
			key := strings.Trim(strings.TrimSpace(parts[0]), `"`)
			val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			result[key] = val
		}
	}
	
	return result, nil
}

// ConvertAdvancedValue converts values with advanced type support
func (atm *AdvancedTypeMapper) ConvertAdvancedValue(value interface{}, typeOID uint32) (interface{}, error) {
	// Check if it's an array type
	if _, isArray := atm.arrayTypeCache[typeOID]; isArray {
		// Handle as array of the element type
		if arrayType, exists := atm.typeMap[typeOID]; exists {
			return arrayType.Handler(value)
		}
		// Fallback: parse as generic array
		return atm.handleGenericArray(value)
	}
	
	// Check custom types
	if enumType, exists := atm.enumTypes[typeOID]; exists {
		_ = enumType // Use enum metadata if needed
		return atm.handleEnum(value)
	}
	
	if compositeType, exists := atm.compositeTypes[typeOID]; exists {
		_ = compositeType // Use composite metadata if needed
		return atm.handleComposite(value)
	}
	
	if domainType, exists := atm.domainTypes[typeOID]; exists {
		// Use base type conversion
		return atm.ConvertValue(value, domainType.BaseTypeOID)
	}
	
	// Fall back to basic type conversion
	return atm.ConvertValue(value, typeOID)
}
