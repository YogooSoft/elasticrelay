package logger

import (
	"log"
	"strings"
	"sync"
)

// LogLevel represents the log levels
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var (
	currentLogLevel LogLevel = INFO // Default to INFO
	logMutex        sync.RWMutex
)

// SetLogLevel sets the global log level
func SetLogLevel(level string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	
	switch strings.ToLower(level) {
	case "debug":
		currentLogLevel = DEBUG
	case "info":
		currentLogLevel = INFO
	case "warn", "warning":
		currentLogLevel = WARN
	case "error":
		currentLogLevel = ERROR
	default:
		currentLogLevel = INFO
	}
}

// GetLogLevel returns the current log level
func GetLogLevel() LogLevel {
	logMutex.RLock()
	defer logMutex.RUnlock()
	return currentLogLevel
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	if GetLogLevel() <= DEBUG {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	if GetLogLevel() <= INFO {
		log.Printf(format, args...)
	}
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	if GetLogLevel() <= WARN {
		log.Printf("[WARN] "+format, args...)
	}
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	if GetLogLevel() <= ERROR {
		log.Printf("[ERROR] "+format, args...)
	}
}

// Printf is a direct passthrough to log.Printf (for compatibility)
func Printf(format string, args ...interface{}) {
	log.Printf(format, args...)
}
