package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Supported configuration sources.
const (
	SourceFile  = "file"
	SourceNacos = "nacos"
)

// Nacos startup policies used when the server is unreachable at startup.
const (
	StartupFailFast = "fail_fast"
	StartupUseCache = "use_cache"
)

// Apply modes for configuration changes detected at runtime.
const (
	ApplyModeAuto   = "auto"
	ApplyModeDryRun = "dry_run"
)

// Options holds all configuration-source related startup options.
// Every option can be set by a command-line flag or an environment variable;
// command-line flags take precedence over environment variables.
type Options struct {
	Source          string
	ConfigFile      string
	TransformConfig string
	Watch           bool
	ExpandEnv       bool
	Debounce        time.Duration
	ApplyMode       string
	Nacos           NacosOptions
}

// NacosOptions holds the Nacos connection settings.
type NacosOptions struct {
	ServerAddr      string
	Namespace       string
	Group           string
	DataID          string
	TransformDataID string
	Username        string
	Password        string
	AccessKey       string
	SecretKey       string
	ContextPath     string
	Timeout         time.Duration
	CacheDir        string
	LogDir          string
	LogLevel        string
	StartupPolicy   string
}

// RegisterFlags registers all configuration-source flags on fs. Defaults are
// taken from environment variables so that flags override env, and env
// overrides built-in defaults.
func RegisterFlags(fs *flag.FlagSet) *Options {
	o := &Options{}

	fs.StringVar(&o.Source, "config-source", envString("ELASTICRELAY_CONFIG_SOURCE", SourceFile), "Configuration source: file | nacos")
	fs.StringVar(&o.ConfigFile, "config", envString("ELASTICRELAY_CONFIG", "config.json"), "Path to the configuration file (file source)")
	fs.StringVar(&o.TransformConfig, "transform-config", envString("ELASTICRELAY_TRANSFORM_CONFIG", ""), "Path to the transform configuration file (file source, optional)")
	fs.BoolVar(&o.Watch, "config-watch", envBool("ELASTICRELAY_CONFIG_WATCH", false), "Hot reload local configuration files on change (file source)")
	fs.BoolVar(&o.ExpandEnv, "config-expand-env", envBool("ELASTICRELAY_CONFIG_EXPAND_ENV", false), "Expand ${VAR} placeholders in password fields (always enabled for nacos source)")
	fs.DurationVar(&o.Debounce, "config-debounce", envDuration("ELASTICRELAY_CONFIG_DEBOUNCE", 2*time.Second), "Debounce window for configuration changes")
	fs.StringVar(&o.ApplyMode, "config-apply-mode", envString("ELASTICRELAY_CONFIG_APPLY_MODE", ApplyModeAuto), "How runtime configuration changes are handled: auto | dry_run")

	fs.StringVar(&o.Nacos.ServerAddr, "nacos-server-addr", envString("NACOS_SERVER_ADDR", ""), "Nacos server addresses, comma separated, e.g. 10.0.0.1:8848,10.0.0.2:8848")
	fs.StringVar(&o.Nacos.Namespace, "nacos-namespace", envString("NACOS_NAMESPACE", ""), "Nacos namespace ID (empty means public)")
	fs.StringVar(&o.Nacos.Group, "nacos-group", envString("NACOS_GROUP", "ELASTICRELAY"), "Nacos config group")
	fs.StringVar(&o.Nacos.DataID, "nacos-data-id", envString("NACOS_DATA_ID", "elasticrelay.json"), "Nacos data ID of the main configuration")
	fs.StringVar(&o.Nacos.TransformDataID, "nacos-transform-data-id", envString("NACOS_TRANSFORM_DATA_ID", ""), "Nacos data ID of the transform configuration (optional)")
	fs.StringVar(&o.Nacos.Username, "nacos-username", envString("NACOS_USERNAME", ""), "Nacos username")
	fs.StringVar(&o.Nacos.Password, "nacos-password", envString("NACOS_PASSWORD", ""), "Nacos password (prefer the NACOS_PASSWORD environment variable)")
	fs.StringVar(&o.Nacos.AccessKey, "nacos-access-key", envString("NACOS_ACCESS_KEY", ""), "Nacos access key")
	fs.StringVar(&o.Nacos.SecretKey, "nacos-secret-key", envString("NACOS_SECRET_KEY", ""), "Nacos secret key")
	fs.StringVar(&o.Nacos.ContextPath, "nacos-context-path", envString("NACOS_CONTEXT_PATH", "/nacos"), "Nacos server context path")
	fs.DurationVar(&o.Nacos.Timeout, "nacos-timeout", envDuration("NACOS_TIMEOUT", 5*time.Second), "Nacos request timeout")
	fs.StringVar(&o.Nacos.CacheDir, "nacos-cache-dir", envString("NACOS_CACHE_DIR", "./data/nacos/cache"), "Nacos local snapshot directory")
	fs.StringVar(&o.Nacos.LogDir, "nacos-log-dir", envString("NACOS_LOG_DIR", "./logs/nacos"), "Nacos SDK log directory")
	fs.StringVar(&o.Nacos.LogLevel, "nacos-log-level", envString("NACOS_LOG_LEVEL", "warn"), "Nacos SDK log level: debug | info | warn | error")
	fs.StringVar(&o.Nacos.StartupPolicy, "nacos-startup-policy", envString("NACOS_STARTUP_POLICY", StartupFailFast), "Behavior when Nacos is unavailable at startup: fail_fast | use_cache")

	return o
}

// Validate checks option consistency. fs is used to detect flags that were
// set explicitly; it may be nil. Returned warnings are non-fatal.
func (o *Options) Validate(fs *flag.FlagSet) ([]string, error) {
	var warnings []string

	o.Source = strings.ToLower(strings.TrimSpace(o.Source))
	switch o.Source {
	case SourceFile, SourceNacos:
	default:
		return nil, fmt.Errorf("unsupported config source %q, must be %q or %q", o.Source, SourceFile, SourceNacos)
	}

	switch o.ApplyMode {
	case ApplyModeAuto, ApplyModeDryRun:
	default:
		return nil, fmt.Errorf("unsupported config apply mode %q, must be %q or %q", o.ApplyMode, ApplyModeAuto, ApplyModeDryRun)
	}

	if o.Debounce < 0 {
		return nil, fmt.Errorf("config debounce must not be negative")
	}

	if o.Source == SourceFile {
		return warnings, nil
	}

	if strings.TrimSpace(o.Nacos.ServerAddr) == "" {
		return nil, fmt.Errorf("nacos server address is required when config source is nacos (--nacos-server-addr or NACOS_SERVER_ADDR)")
	}
	if strings.TrimSpace(o.Nacos.DataID) == "" {
		return nil, fmt.Errorf("nacos data ID is required when config source is nacos (--nacos-data-id or NACOS_DATA_ID)")
	}
	if strings.TrimSpace(o.Nacos.Group) == "" {
		return nil, fmt.Errorf("nacos group must not be empty")
	}
	switch o.Nacos.StartupPolicy {
	case StartupFailFast, StartupUseCache:
	default:
		return nil, fmt.Errorf("unsupported nacos startup policy %q, must be %q or %q", o.Nacos.StartupPolicy, StartupFailFast, StartupUseCache)
	}

	if fs != nil {
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "config", "transform-config", "config-watch":
				warnings = append(warnings, fmt.Sprintf("--%s is ignored when config source is nacos", f.Name))
			}
		})
	}
	if o.Nacos.Password != "" && fs != nil {
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "nacos-password" {
				warnings = append(warnings, "--nacos-password is visible in the process list, prefer the NACOS_PASSWORD environment variable")
			}
		})
	}

	return warnings, nil
}

// WatchEnabled reports whether runtime configuration changes should be watched.
func (o *Options) WatchEnabled() bool {
	return o.Source == SourceNacos || o.Watch
}

// ExpandEnvEnabled reports whether ${VAR} placeholders should be expanded.
func (o *Options) ExpandEnvEnabled() bool {
	return o.Source == SourceNacos || o.ExpandEnv
}

// Describe returns a human-readable, secret-free description of the options.
func (o *Options) Describe() string {
	if o.Source == SourceFile {
		desc := fmt.Sprintf("file (config=%s", o.ConfigFile)
		if o.TransformConfig != "" {
			desc += fmt.Sprintf(", transform=%s", o.TransformConfig)
		}
		return desc + fmt.Sprintf(", watch=%t)", o.Watch)
	}

	n := o.Nacos
	desc := fmt.Sprintf("nacos (server=%s, namespace=%s, group=%s, dataId=%s", n.ServerAddr, displayNamespace(n.Namespace), n.Group, n.DataID)
	if n.TransformDataID != "" {
		desc += fmt.Sprintf(", transformDataId=%s", n.TransformDataID)
	}
	auth := "none"
	switch {
	case n.Username != "":
		auth = "user:" + n.Username
	case n.AccessKey != "":
		auth = "aksk"
	}
	return desc + fmt.Sprintf(", auth=%s, startupPolicy=%s, applyMode=%s)", auth, n.StartupPolicy, o.ApplyMode)
}

func displayNamespace(ns string) string {
	if ns == "" {
		return "public"
	}
	return ns
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("Warning: invalid boolean value %q for %s, using default %t", v, key, def)
		return def
	}
	return b
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("Warning: invalid duration value %q for %s, using default %s", v, key, def)
		return def
	}
	return d
}
