package mongodb

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestConvertBSONToMap(t *testing.T) {
	tests := []struct {
		name     string
		input    bson.M
		expected map[string]interface{}
	}{
		{
			name:     "empty document",
			input:    bson.M{},
			expected: map[string]interface{}{},
		},
		{
			name: "simple string field",
			input: bson.M{
				"name": "test",
			},
			expected: map[string]interface{}{
				"name": "test",
			},
		},
		{
			name: "multiple primitive types",
			input: bson.M{
				"string":  "hello",
				"int":     int64(42),
				"float":   3.14,
				"bool":    true,
				"null":    nil,
			},
			expected: map[string]interface{}{
				"string":  "hello",
				"int":     int64(42),
				"float":   3.14,
				"bool":    true,
				"null":    nil,
			},
		},
		{
			name: "int32 normalized to int64",
			input: bson.M{
				"value": int32(100),
			},
			expected: map[string]interface{}{
				"value": int64(100),
			},
		},
		{
			name: "float32 normalized to float64",
			input: bson.M{
				"value": float32(1.5),
			},
			expected: map[string]interface{}{
				"value": float64(float32(1.5)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertBSONToMap(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("convertBSONToMap() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConvertBSONValue_ObjectID(t *testing.T) {
	objectID, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	result := convertBSONValue(objectID)
	expected := "507f1f77bcf86cd799439011"

	if result != expected {
		t.Errorf("convertBSONValue(ObjectID) = %v, want %v", result, expected)
	}
}

func TestConvertBSONValue_DateTime(t *testing.T) {
	testTime := time.Date(2023, 6, 15, 10, 30, 0, 0, time.UTC)
	dateTime := primitive.NewDateTimeFromTime(testTime)
	result := convertBSONValue(dateTime)

	// Check that result is a valid RFC3339Nano string
	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("Expected string, got %T", result)
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, resultStr)
	if err != nil {
		t.Fatalf("Failed to parse time: %v", err)
	}

	if !parsedTime.Equal(testTime) {
		t.Errorf("Parsed time %v != expected %v", parsedTime, testTime)
	}
}

func TestConvertBSONValue_Timestamp(t *testing.T) {
	ts := primitive.Timestamp{T: 1623456789, I: 1}
	result := convertBSONValue(ts)

	expected := map[string]interface{}{
		"t": uint32(1623456789),
		"i": uint32(1),
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("convertBSONValue(Timestamp) = %v, want %v", result, expected)
	}
}

func TestConvertBSONValue_Binary(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	binary := primitive.Binary{
		Subtype: 0x00,
		Data:    data,
	}
	result := convertBSONValue(binary)

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", result)
	}

	binaryPart, ok := resultMap["$binary"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected $binary map, got %T", resultMap["$binary"])
	}

	expectedBase64 := base64.StdEncoding.EncodeToString(data)
	if binaryPart["base64"] != expectedBase64 {
		t.Errorf("base64 = %v, want %v", binaryPart["base64"], expectedBase64)
	}

	if binaryPart["subType"] != "00" {
		t.Errorf("subType = %v, want '00'", binaryPart["subType"])
	}
}

func TestConvertBSONValue_Decimal128(t *testing.T) {
	decimal, _ := primitive.ParseDecimal128("123.456789")
	result := convertBSONValue(decimal)

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("Expected string, got %T", result)
	}

	// Decimal128 preserves the string representation
	if resultStr != "123.456789" {
		t.Errorf("Decimal128 = %v, want '123.456789'", resultStr)
	}
}

func TestConvertBSONValue_Regex(t *testing.T) {
	regex := primitive.Regex{
		Pattern: "^test.*$",
		Options: "im",
	}
	result := convertBSONValue(regex)

	expected := map[string]interface{}{
		"$regex":   "^test.*$",
		"$options": "im",
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("convertBSONValue(Regex) = %v, want %v", result, expected)
	}
}

func TestConvertBSONValue_JavaScript(t *testing.T) {
	js := primitive.JavaScript("function() { return 1; }")
	result := convertBSONValue(js)

	if result != "function() { return 1; }" {
		t.Errorf("convertBSONValue(JavaScript) = %v, want 'function() { return 1; }'", result)
	}
}

func TestConvertBSONValue_CodeWithScope(t *testing.T) {
	codeWithScope := primitive.CodeWithScope{
		Code:  "function(x) { return x + y; }",
		Scope: bson.M{"y": 10},
	}
	result := convertBSONValue(codeWithScope)

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", result)
	}

	if resultMap["$code"] != "function(x) { return x + y; }" {
		t.Errorf("$code = %v, want 'function(x) { return x + y; }'", resultMap["$code"])
	}

	scopeMap, ok := resultMap["$scope"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected scope map, got %T", resultMap["$scope"])
	}

	// The value could be int or int64 depending on BSON library version
	switch v := scopeMap["y"].(type) {
	case int:
		if v != 10 {
			t.Errorf("scope.y = %v, want 10", v)
		}
	case int64:
		if v != 10 {
			t.Errorf("scope.y = %v, want 10", v)
		}
	default:
		t.Errorf("scope.y has unexpected type %T", scopeMap["y"])
	}
}

func TestConvertBSONValue_DBPointer(t *testing.T) {
	objectID, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	dbPointer := primitive.DBPointer{
		DB:      "testdb",
		Pointer: objectID,
	}
	result := convertBSONValue(dbPointer)

	expected := map[string]interface{}{
		"$ref": "testdb",
		"$id":  "507f1f77bcf86cd799439011",
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("convertBSONValue(DBPointer) = %v, want %v", result, expected)
	}
}

func TestConvertBSONValue_Symbol(t *testing.T) {
	symbol := primitive.Symbol("my_symbol")
	result := convertBSONValue(symbol)

	if result != "my_symbol" {
		t.Errorf("convertBSONValue(Symbol) = %v, want 'my_symbol'", result)
	}
}

func TestConvertBSONValue_MinMaxKey(t *testing.T) {
	minKeyResult := convertBSONValue(primitive.MinKey{})
	expectedMinKey := map[string]interface{}{"$minKey": 1}
	if !reflect.DeepEqual(minKeyResult, expectedMinKey) {
		t.Errorf("convertBSONValue(MinKey) = %v, want %v", minKeyResult, expectedMinKey)
	}

	maxKeyResult := convertBSONValue(primitive.MaxKey{})
	expectedMaxKey := map[string]interface{}{"$maxKey": 1}
	if !reflect.DeepEqual(maxKeyResult, expectedMaxKey) {
		t.Errorf("convertBSONValue(MaxKey) = %v, want %v", maxKeyResult, expectedMaxKey)
	}
}

func TestConvertBSONValue_UndefinedAndNull(t *testing.T) {
	undefinedResult := convertBSONValue(primitive.Undefined{})
	if undefinedResult != nil {
		t.Errorf("convertBSONValue(Undefined) = %v, want nil", undefinedResult)
	}

	nullResult := convertBSONValue(primitive.Null{})
	if nullResult != nil {
		t.Errorf("convertBSONValue(Null) = %v, want nil", nullResult)
	}
}

func TestConvertBSONValue_NestedDocument(t *testing.T) {
	nested := bson.M{
		"level1": bson.M{
			"level2": bson.M{
				"value": "deep",
			},
		},
	}
	result := convertBSONValue(nested)

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", result)
	}

	level1, ok := resultMap["level1"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected level1 to be map, got %T", resultMap["level1"])
	}

	level2, ok := level1["level2"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected level2 to be map, got %T", level1["level2"])
	}

	if level2["value"] != "deep" {
		t.Errorf("level2.value = %v, want 'deep'", level2["value"])
	}
}

func TestConvertBSONValue_Array(t *testing.T) {
	arr := bson.A{"one", int64(2), true, nil}
	result := convertBSONValue(arr)

	resultArr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("Expected []interface{}, got %T", result)
	}

	expected := []interface{}{"one", int64(2), true, nil}
	if !reflect.DeepEqual(resultArr, expected) {
		t.Errorf("convertBSONValue(array) = %v, want %v", resultArr, expected)
	}
}

func TestConvertBSONValue_BsonD(t *testing.T) {
	doc := bson.D{
		{Key: "a", Value: 1},
		{Key: "b", Value: "two"},
		{Key: "c", Value: true},
	}
	result := convertBSONValue(doc)

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", result)
	}

	// The value could be int or int64 depending on BSON library version
	switch v := resultMap["a"].(type) {
	case int:
		if v != 1 {
			t.Errorf("doc.a = %v, want 1", v)
		}
	case int64:
		if v != 1 {
			t.Errorf("doc.a = %v, want 1", v)
		}
	default:
		t.Errorf("doc.a has unexpected type %T", resultMap["a"])
	}
	if resultMap["b"] != "two" {
		t.Errorf("doc.b = %v, want 'two'", resultMap["b"])
	}
	if resultMap["c"] != true {
		t.Errorf("doc.c = %v, want true", resultMap["c"])
	}
}

func TestConvertBSONValue_GoTime(t *testing.T) {
	testTime := time.Date(2023, 6, 15, 10, 30, 0, 123456789, time.UTC)
	result := convertBSONValue(testTime)

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("Expected string, got %T", result)
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, resultStr)
	if err != nil {
		t.Fatalf("Failed to parse time: %v", err)
	}

	if !parsedTime.Equal(testTime) {
		t.Errorf("Parsed time %v != expected %v", parsedTime, testTime)
	}
}

func TestConvertBSONValue_ByteSlice(t *testing.T) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	result := convertBSONValue(data)

	expected := base64.StdEncoding.EncodeToString(data)
	if result != expected {
		t.Errorf("convertBSONValue([]byte) = %v, want %v", result, expected)
	}
}

func TestGetPrimaryKey(t *testing.T) {
	tests := []struct {
		name     string
		docKey   bson.M
		expected string
	}{
		{
			name:     "nil document key",
			docKey:   nil,
			expected: "",
		},
		{
			name:     "missing _id",
			docKey:   bson.M{"other": "value"},
			expected: "",
		},
		{
			name: "ObjectID _id",
			docKey: bson.M{
				"_id": func() primitive.ObjectID {
					id, _ := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
					return id
				}(),
			},
			expected: "507f1f77bcf86cd799439011",
		},
		{
			name:     "string _id",
			docKey:   bson.M{"_id": "custom-id-123"},
			expected: "custom-id-123",
		},
		{
			name:     "int _id",
			docKey:   bson.M{"_id": 12345},
			expected: "12345",
		},
		{
			name:     "int32 _id",
			docKey:   bson.M{"_id": int32(67890)},
			expected: "67890",
		},
		{
			name:     "int64 _id",
			docKey:   bson.M{"_id": int64(9876543210)},
			expected: "9876543210",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPrimaryKey(tt.docKey)
			if result != tt.expected {
				t.Errorf("getPrimaryKey() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetPrimaryKey_ComplexKey(t *testing.T) {
	// Complex key should be serialized as JSON
	docKey := bson.M{
		"_id": map[string]interface{}{
			"a": 1,
			"b": "two",
		},
	}
	result := getPrimaryKey(docKey)

	// Parse result as JSON to verify it's valid
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("Failed to parse complex key as JSON: %v", err)
	}

	// Verify the structure (JSON order may vary)
	if parsed["a"] != float64(1) { // JSON unmarshals numbers as float64
		t.Errorf("parsed.a = %v, want 1", parsed["a"])
	}
	if parsed["b"] != "two" {
		t.Errorf("parsed.b = %v, want 'two'", parsed["b"])
	}
}

func TestEncodeDecodeResumeToken(t *testing.T) {
	// Create a sample resume token (simulating a real one)
	originalToken := bson.Raw([]byte{0x01, 0x02, 0x03, 0x04, 0x05})

	// Encode
	encoded := encodeResumeToken(originalToken)
	if encoded == "" {
		t.Fatal("encodeResumeToken returned empty string")
	}

	// Decode
	decoded, err := decodeResumeToken(encoded)
	if err != nil {
		t.Fatalf("decodeResumeToken failed: %v", err)
	}

	// Verify roundtrip
	if !reflect.DeepEqual([]byte(decoded), []byte(originalToken)) {
		t.Errorf("Decoded token %v != original %v", decoded, originalToken)
	}
}

func TestEncodeResumeToken_Nil(t *testing.T) {
	result := encodeResumeToken(nil)
	if result != "" {
		t.Errorf("encodeResumeToken(nil) = %v, want empty string", result)
	}
}

func TestDecodeResumeToken_Empty(t *testing.T) {
	_, err := decodeResumeToken("")
	if err == nil {
		t.Error("decodeResumeToken('') should return error")
	}
}

func TestDecodeResumeToken_InvalidBase64(t *testing.T) {
	_, err := decodeResumeToken("not-valid-base64!!!")
	if err == nil {
		t.Error("decodeResumeToken with invalid base64 should return error")
	}
}

func TestFlattenDocument(t *testing.T) {
	tests := []struct {
		name     string
		doc      map[string]interface{}
		prefix   string
		maxDepth int
		expected map[string]interface{}
	}{
		{
			name: "simple flat document",
			doc: map[string]interface{}{
				"name": "test",
				"age":  25,
			},
			prefix:   "",
			maxDepth: 3,
			expected: map[string]interface{}{
				"name": "test",
				"age":  25,
			},
		},
		{
			name: "nested document within depth",
			doc: map[string]interface{}{
				"user": map[string]interface{}{
					"name": "John",
					"age":  30,
				},
			},
			prefix:   "",
			maxDepth: 3,
			expected: map[string]interface{}{
				"user.name": "John",
				"user.age":  30,
			},
		},
		{
			name: "deeply nested document",
			doc: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": map[string]interface{}{
							"value": "deep",
						},
					},
				},
			},
			prefix:   "",
			maxDepth: 3,
			expected: map[string]interface{}{
				"level1.level2.level3.value": "deep",
			},
		},
		{
			name: "max depth boundary - stops at depth",
			doc: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": map[string]interface{}{
							"value": "deep",
						},
					},
				},
			},
			prefix:   "",
			maxDepth: 1,
			expected: map[string]interface{}{
				"level1.level2": map[string]interface{}{
					"level3": map[string]interface{}{
						"value": "deep",
					},
				},
			},
		},
		{
			name: "max depth 0 - no flattening of nested",
			doc: map[string]interface{}{
				"user": map[string]interface{}{
					"name": "John",
				},
			},
			prefix:   "",
			maxDepth: 0,
			expected: map[string]interface{}{
				"user": map[string]interface{}{
					"name": "John",
				},
			},
		},
		{
			name: "_id is preserved at root level",
			doc: map[string]interface{}{
				"_id":  "12345",
				"name": "test",
				"nested": map[string]interface{}{
					"field": "value",
				},
			},
			prefix:   "",
			maxDepth: 3,
			expected: map[string]interface{}{
				"_id":          "12345",
				"name":         "test",
				"nested.field": "value",
			},
		},
		{
			name: "arrays are preserved",
			doc: map[string]interface{}{
				"tags": []interface{}{"a", "b", "c"},
				"nested": map[string]interface{}{
					"items": []interface{}{1, 2, 3},
				},
			},
			prefix:   "",
			maxDepth: 3,
			expected: map[string]interface{}{
				"tags":         []interface{}{"a", "b", "c"},
				"nested.items": []interface{}{1, 2, 3},
			},
		},
		{
			name: "with prefix",
			doc: map[string]interface{}{
				"field": "value",
			},
			prefix:   "root",
			maxDepth: 3,
			expected: map[string]interface{}{
				"root.field": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FlattenDocument(tt.doc, tt.prefix, tt.maxDepth)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("FlattenDocument() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFlattenDocument_MixedTypes(t *testing.T) {
	doc := map[string]interface{}{
		"_id": "test-id",
		"metadata": map[string]interface{}{
			"created": "2023-01-01",
			"author": map[string]interface{}{
				"name":  "John",
				"email": "john@example.com",
			},
		},
		"tags":    []interface{}{"tag1", "tag2"},
		"enabled": true,
		"count":   42,
	}

	result := FlattenDocument(doc, "", 10)

	expected := map[string]interface{}{
		"_id":                    "test-id",
		"metadata.created":       "2023-01-01",
		"metadata.author.name":   "John",
		"metadata.author.email":  "john@example.com",
		"tags":                   []interface{}{"tag1", "tag2"},
		"enabled":                true,
		"count":                  42,
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("FlattenDocument() = %v, want %v", result, expected)
	}
}

// Benchmark tests
func BenchmarkConvertBSONToMap(b *testing.B) {
	doc := bson.M{
		"_id":     primitive.NewObjectID(),
		"name":    "benchmark test",
		"count":   int64(100),
		"enabled": true,
		"nested": bson.M{
			"field1": "value1",
			"field2": int64(200),
		},
		"array": bson.A{"item1", "item2", "item3"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		convertBSONToMap(doc)
	}
}

func BenchmarkFlattenDocument(b *testing.B) {
	doc := map[string]interface{}{
		"_id":  "test",
		"name": "benchmark",
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"level3": map[string]interface{}{
					"value": "deep",
				},
			},
		},
		"array": []interface{}{"a", "b", "c"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FlattenDocument(doc, "", 5)
	}
}
