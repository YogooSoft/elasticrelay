package mongodb

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// convertBSONToMap converts a BSON document to a JSON-friendly map
// This handles special MongoDB types like ObjectId, Date, Binary, etc.
func convertBSONToMap(doc bson.M) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range doc {
		result[key] = convertBSONValue(value)
	}
	return result
}

// convertBSONValue converts a single BSON value to a JSON-friendly value
func convertBSONValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case primitive.ObjectID:
		// Convert ObjectId to string
		return v.Hex()

	case primitive.DateTime:
		// Convert DateTime to RFC3339 string
		return v.Time().UTC().Format(time.RFC3339Nano)

	case primitive.Timestamp:
		// Convert Timestamp to ISO string with details
		return map[string]interface{}{
			"t": v.T,
			"i": v.I,
		}

	case primitive.Binary:
		// Convert Binary to base64 string
		return map[string]interface{}{
			"$binary": map[string]interface{}{
				"base64":  base64.StdEncoding.EncodeToString(v.Data),
				"subType": fmt.Sprintf("%02x", v.Subtype),
			},
		}

	case primitive.Decimal128:
		// Convert Decimal128 to string to preserve precision
		return v.String()

	case primitive.Regex:
		// Convert Regex to string representation
		return map[string]interface{}{
			"$regex":   v.Pattern,
			"$options": v.Options,
		}

	case primitive.JavaScript:
		// Convert JavaScript code to string
		return string(v)

	case primitive.CodeWithScope:
		// Convert CodeWithScope to object
		scopeMap := make(map[string]interface{})
		if v.Scope != nil {
			if scope, ok := v.Scope.(bson.M); ok {
				scopeMap = convertBSONToMap(scope)
			} else if scope, ok := v.Scope.(map[string]interface{}); ok {
				for key, val := range scope {
					scopeMap[key] = convertBSONValue(val)
				}
			}
		}
		return map[string]interface{}{
			"$code":  string(v.Code),
			"$scope": scopeMap,
		}

	case primitive.DBPointer:
		// Convert DBPointer to object (deprecated but may exist)
		return map[string]interface{}{
			"$ref": v.DB,
			"$id":  v.Pointer.Hex(),
		}

	case primitive.Symbol:
		// Convert Symbol to string
		return string(v)

	case primitive.MinKey:
		return map[string]interface{}{"$minKey": 1}

	case primitive.MaxKey:
		return map[string]interface{}{"$maxKey": 1}

	case primitive.Undefined:
		return nil

	case primitive.Null:
		return nil

	case bson.M:
		// Recursively convert nested documents
		return convertBSONToMap(v)

	case bson.D:
		// Convert ordered document to map
		result := make(map[string]interface{})
		for _, elem := range v {
			result[elem.Key] = convertBSONValue(elem.Value)
		}
		return result

	case bson.A:
		// Convert array
		result := make([]interface{}, len(v))
		for i, elem := range v {
			result[i] = convertBSONValue(elem)
		}
		return result

	case []interface{}:
		// Convert slice
		result := make([]interface{}, len(v))
		for i, elem := range v {
			result[i] = convertBSONValue(elem)
		}
		return result

	case map[string]interface{}:
		// Convert map
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = convertBSONValue(val)
		}
		return result

	case time.Time:
		// Convert Go time to RFC3339 string
		return v.UTC().Format(time.RFC3339Nano)

	case int32:
		return int64(v) // Normalize to int64 for consistency

	case float32:
		return float64(v) // Normalize to float64 for consistency

	case []byte:
		// Convert byte array to base64 string
		return base64.StdEncoding.EncodeToString(v)

	default:
		// For all other types (string, int, int64, float64, bool), return as-is
		return v
	}
}

// getPrimaryKey extracts the primary key (_id) from document key
func getPrimaryKey(docKey bson.M) string {
	if docKey == nil {
		return ""
	}

	id, ok := docKey["_id"]
	if !ok {
		return ""
	}

	switch v := id.(type) {
	case primitive.ObjectID:
		return v.Hex()
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	default:
		// Try to marshal as JSON for complex keys
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}

// encodeResumeToken encodes a resume token to string for storage
func encodeResumeToken(token bson.Raw) string {
	if token == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(token)
}

// decodeResumeToken decodes a resume token string back to bson.Raw
func decodeResumeToken(tokenStr string) (bson.Raw, error) {
	if tokenStr == "" {
		return nil, fmt.Errorf("empty resume token")
	}

	data, err := base64.StdEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode resume token: %w", err)
	}

	return bson.Raw(data), nil
}

// FlattenDocument flattens nested documents using dot notation
// This is useful for Elasticsearch which handles nested objects differently
func FlattenDocument(doc map[string]interface{}, prefix string, maxDepth int) map[string]interface{} {
	result := make(map[string]interface{})
	flattenRecursive(doc, prefix, result, 0, maxDepth)
	return result
}

func flattenRecursive(doc map[string]interface{}, prefix string, result map[string]interface{}, depth int, maxDepth int) {
	for key, value := range doc {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// Skip _id as it's handled separately
		if key == "_id" && prefix == "" {
			result[key] = value
			continue
		}

		switch v := value.(type) {
		case map[string]interface{}:
			if depth < maxDepth {
				// Recursively flatten nested documents
				flattenRecursive(v, fullKey, result, depth+1, maxDepth)
			} else {
				// Max depth reached, keep as nested object
				result[fullKey] = v
			}
		case []interface{}:
			// Keep arrays as-is (Elasticsearch can handle arrays)
			result[fullKey] = v
		default:
			result[fullKey] = value
		}
	}
}
