package parallel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SimpleESClient is a basic Elasticsearch client implementation
type SimpleESClient struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// NewSimpleESClient creates a new simple ES client
func NewSimpleESClient(baseURL, username, password string) *SimpleESClient {
	return &SimpleESClient{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// BulkIndex performs bulk indexing to Elasticsearch
func (c *SimpleESClient) BulkIndex(indexName string, documents []*ESDocument) error {
	if len(documents) == 0 {
		return nil
	}

	// Build bulk request body
	var bulkBody strings.Builder

	for _, doc := range documents {
		// Index operation
		indexOp := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
				"_id":    doc.ID,
			},
		}
		indexOpJSON, _ := json.Marshal(indexOp)
		bulkBody.Write(indexOpJSON)
		bulkBody.WriteString("\n")

		// Document source
		sourceJSON, err := json.Marshal(doc.Source)
		if err != nil {
			return fmt.Errorf("failed to marshal document source: %w", err)
		}
		bulkBody.Write(sourceJSON)
		bulkBody.WriteString("\n")
	}

	// Send bulk request
	url := fmt.Sprintf("%s/_bulk", c.baseURL)
	req, err := http.NewRequest("POST", url, strings.NewReader(bulkBody.String()))
	if err != nil {
		return fmt.Errorf("failed to create bulk request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-ndjson")
	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bulk request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bulk request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response to check for errors
	var bulkResp BulkResponse
	if err := json.NewDecoder(resp.Body).Decode(&bulkResp); err != nil {
		return fmt.Errorf("failed to parse bulk response: %w", err)
	}

	if bulkResp.Errors {
		return fmt.Errorf("bulk indexing completed with errors")
	}

	return nil
}

// CreateIndex creates an index if it doesn't exist
func (c *SimpleESClient) CreateIndex(indexName string) error {
	url := fmt.Sprintf("%s/%s", c.baseURL, indexName)

	// Check if index exists
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HEAD request: %w", err)
	}

	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check index existence: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil // Index already exists
	}

	// Create index
	indexSettings := map[string]interface{}{
		"settings": map[string]interface{}{
			"number_of_shards":   1,
			"number_of_replicas": 0,
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"_table": map[string]interface{}{
					"type": "keyword",
				},
				"_timestamp": map[string]interface{}{
					"type": "long",
				},
			},
		},
	}

	settingsJSON, err := json.Marshal(indexSettings)
	if err != nil {
		return fmt.Errorf("failed to marshal index settings: %w", err)
	}

	req, err = http.NewRequest("PUT", url, bytes.NewReader(settingsJSON))
	if err != nil {
		return fmt.Errorf("failed to create PUT request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create index failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetIndexStats returns statistics about an index
func (c *SimpleESClient) GetIndexStats(indexName string) (*IndexStats, error) {
	url := fmt.Sprintf("%s/%s/_stats", c.baseURL, indexName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %w", err)
	}

	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get index stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("get index stats failed with status %d", resp.StatusCode)
	}

	var statsResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statsResp); err != nil {
		return nil, fmt.Errorf("failed to parse stats response: %w", err)
	}

	// Extract basic stats (simplified)
	stats := &IndexStats{
		IndexName: indexName,
	}

	if indices, ok := statsResp["indices"].(map[string]interface{}); ok {
		if indexStats, ok := indices[indexName].(map[string]interface{}); ok {
			if primaries, ok := indexStats["primaries"].(map[string]interface{}); ok {
				if docs, ok := primaries["docs"].(map[string]interface{}); ok {
					if count, ok := docs["count"].(float64); ok {
						stats.DocumentCount = int64(count)
					}
				}
			}
		}
	}

	return stats, nil
}

// BulkResponse represents the response from a bulk operation
type BulkResponse struct {
	Took   int                      `json:"took"`
	Errors bool                     `json:"errors"`
	Items  []map[string]interface{} `json:"items"`
}

// IndexStats represents index statistics
type IndexStats struct {
	IndexName     string `json:"index_name"`
	DocumentCount int64  `json:"document_count"`
}
