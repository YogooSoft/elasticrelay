package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
)

// DetectFormatFromBytes detects the configuration format version of raw JSON content.
func DetectFormatFromBytes(data []byte) (string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "unknown", fmt.Errorf("configuration content is empty")
	}

	var version ConfigVersion
	if err := json.Unmarshal(data, &version); err == nil && version.Version != "" {
		return version.Version, nil
	}

	var legacyCheck struct {
		DBHost string `json:"db_host"`
	}
	if err := json.Unmarshal(data, &legacyCheck); err == nil && legacyCheck.DBHost != "" {
		return "1.0", nil
	}

	return "unknown", fmt.Errorf("unknown configuration format")
}

// ParseMultiConfig parses raw JSON content into a MultiConfig without touching
// any package-level state. Legacy 1.0 content is converted in memory.
func ParseMultiConfig(data []byte) (*MultiConfig, error) {
	format, err := DetectFormatFromBytes(data)
	if err != nil {
		return nil, err
	}

	if format == "1.0" {
		var legacy Config
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, fmt.Errorf("failed to parse legacy configuration: %w", err)
		}
		log.Printf("Warning: legacy 1.0 configuration converted to multi-config format in memory, consider upgrading to version 3.0")
		return MigrateFromLegacyConfig(&legacy), nil
	}

	var cfg MultiConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse multi configuration: %w", err)
	}
	return &cfg, nil
}

var supportedSourceTypes = map[string]bool{"mysql": true, "postgresql": true, "mongodb": true}

// ValidateMultiConfig validates a multi configuration for structural correctness.
func ValidateMultiConfig(config *MultiConfig) error {
	if config == nil {
		return fmt.Errorf("configuration is nil")
	}
	if config.Version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	sourceIDs := make(map[string]bool)
	for _, ds := range config.DataSources {
		if ds.ID == "" {
			return fmt.Errorf("data source ID cannot be empty")
		}
		if sourceIDs[ds.ID] {
			return fmt.Errorf("duplicate data source ID %s", ds.ID)
		}
		sourceIDs[ds.ID] = true
		if !supportedSourceTypes[ds.Type] {
			return fmt.Errorf("unsupported data source type %q for %s", ds.Type, ds.ID)
		}
		if ds.Host == "" {
			return fmt.Errorf("data source host cannot be empty for %s", ds.ID)
		}
		if ds.Port <= 0 || ds.Port > 65535 {
			return fmt.Errorf("data source port must be between 1 and 65535 for %s", ds.ID)
		}
	}

	sinkIDs := make(map[string]bool)
	for _, sink := range config.Sinks {
		if sink.ID == "" {
			return fmt.Errorf("sink ID cannot be empty")
		}
		if sinkIDs[sink.ID] {
			return fmt.Errorf("duplicate sink ID %s", sink.ID)
		}
		sinkIDs[sink.ID] = true
		if len(sink.Addresses) == 0 {
			return fmt.Errorf("sink addresses cannot be empty for %s", sink.ID)
		}
	}

	jobIDs := make(map[string]bool)
	for _, job := range config.Jobs {
		if job.ID == "" {
			return fmt.Errorf("job ID cannot be empty")
		}
		if jobIDs[job.ID] {
			return fmt.Errorf("duplicate job ID %s", job.ID)
		}
		jobIDs[job.ID] = true
		if !sourceIDs[job.SourceID] {
			return fmt.Errorf("job %s references unknown data source %s", job.ID, job.SourceID)
		}
		if !sinkIDs[job.SinkID] {
			return fmt.Errorf("job %s references unknown sink %s", job.ID, job.SinkID)
		}
	}

	return nil
}

// envPlaceholder matches a ${NAME} placeholder. Bare $NAME is intentionally not
// supported so that passwords containing '$' are left untouched.
var envPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandSecrets replaces ${NAME} placeholders in password fields with the value
// of the corresponding environment variable. It fails if a variable is undefined.
func ExpandSecrets(cfg *MultiConfig) error {
	expand := func(owner, value string) (string, error) {
		var missing []string
		out := envPlaceholder.ReplaceAllStringFunc(value, func(m string) string {
			name := envPlaceholder.FindStringSubmatch(m)[1]
			v, ok := os.LookupEnv(name)
			if !ok {
				missing = append(missing, name)
				return m
			}
			return v
		})
		if len(missing) > 0 {
			return "", fmt.Errorf("%s password references undefined environment variable(s): %v", owner, missing)
		}
		return out, nil
	}

	for i := range cfg.DataSources {
		v, err := expand("data source "+cfg.DataSources[i].ID, cfg.DataSources[i].Password)
		if err != nil {
			return err
		}
		cfg.DataSources[i].Password = v
	}
	for i := range cfg.Sinks {
		v, err := expand("sink "+cfg.Sinks[i].ID, cfg.Sinks[i].Password)
		if err != nil {
			return err
		}
		cfg.Sinks[i].Password = v
	}
	return nil
}

// SetCurrentMultiConfig replaces the package-level multi configuration so that
// GetMultiConfig and friends observe configurations applied at runtime.
func SetCurrentMultiConfig(cfg *MultiConfig) {
	multiCfgMux.Lock()
	defer multiCfgMux.Unlock()
	multiCfg = cfg
	multiOnce.Do(func() {})
}

// MaskedMultiConfig returns a deep copy of cfg with all secrets replaced.
func MaskedMultiConfig(cfg *MultiConfig) *MultiConfig {
	if cfg == nil {
		return nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var cp MultiConfig
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil
	}
	for i := range cp.DataSources {
		cp.DataSources[i].Password = maskSecret(cp.DataSources[i].Password)
	}
	for i := range cp.Sinks {
		cp.Sinks[i].Password = maskSecret(cp.Sinks[i].Password)
	}
	return &cp
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return "******"
}
