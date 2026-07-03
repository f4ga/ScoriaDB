// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package logger provides a structured logging system with levels (DEBUG, INFO, WARN, ERROR)
// and source location tracking. It is designed for production use in ScoriaDB.
package logger

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
)

// Level represents a log severity level.
type Level int

const (
	// DEBUG level for detailed diagnostic information.
	DEBUG Level = iota
	// INFO level for general operational information.
	INFO
	// WARN level for potentially harmful situations.
	WARN
	// ERROR level for error events that might still allow the application to continue.
	ERROR
)

// Component defines the subsystem that is logging.
type Component string

const (
	ComponentEngine     Component = "engine"
	ComponentCompaction Component = "compaction"
	ComponentFlush      Component = "flush"
	ComponentWAL        Component = "wal"
	ComponentVLog       Component = "vlog"
	ComponentAPI        Component = "api"
	ComponentAuth       Component = "auth"
)

var (
	currentLevel = INFO
	component    = "scoriadb"
)

// skipInfoComponents defines which components should NOT log at INFO level.
var skipInfoComponents = map[Component]bool{
	ComponentCompaction: true,
	ComponentFlush:      true,
}

func init() {
	// Check LOG_LEVEL environment variable on startup.
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG":
		currentLevel = DEBUG
	case "INFO":
		currentLevel = INFO
	case "WARN":
		currentLevel = WARN
	case "ERROR":
		currentLevel = ERROR
	}
}

// SetLevel sets the minimum log level. Messages below this level are suppressed.
func SetLevel(level Level) {
	currentLevel = level
}

// GetLevel returns the current log level.
func GetLevel() Level {
	return currentLevel
}

// SetComponent sets the component name shown in log messages.
func SetComponent(name string) {
	component = name
}

// logMessage writes a formatted log message with level, source location, and component.
func logMessage(level Level, levelStr string, format string, args ...interface{}) {
	if level < currentLevel {
		return
	}
	msg := fmt.Sprintf(format, args...)

	// Add caller information (file:line).
	_, file, line, ok := runtime.Caller(2)
	if ok {
		// Trim the path to the last 3 segments for readability.
		parts := strings.Split(file, "/")
		if len(parts) > 3 {
			file = strings.Join(parts[len(parts)-3:], "/")
		}
		log.Printf("[%s] [%s] %s:%d: %s", component, levelStr, file, line, msg)
	} else {
		log.Printf("[%s] [%s] %s", component, levelStr, msg)
	}
}

// logMessageWithComponent writes a formatted log message with level, component, source location.
func logMessageWithComponent(level Level, comp Component, levelStr string, format string, args ...interface{}) {
	if level < currentLevel {
		return
	}
	// Skip INFO-level logs for components in the skip list.
	if level == INFO && skipInfoComponents[comp] {
		return
	}
	msg := fmt.Sprintf(format, args...)

	_, file, line, ok := runtime.Caller(2)
	if ok {
		parts := strings.Split(file, "/")
		if len(parts) > 3 {
			file = strings.Join(parts[len(parts)-3:], "/")
		}
		log.Printf("[%s] [%s] %s:%d: %s", component, levelStr, file, line, msg)
	} else {
		log.Printf("[%s] [%s] %s", component, levelStr, msg)
	}
}

// Debug logs a message at DEBUG level.
func Debug(format string, args ...interface{}) {
	logMessage(DEBUG, "DEBUG", format, args...)
}

// Info logs a message at INFO level.
func Info(format string, args ...interface{}) {
	logMessage(INFO, "INFO", format, args...)
}

// Warn logs a message at WARN level.
func Warn(format string, args ...interface{}) {
	logMessage(WARN, "WARN", format, args...)
}

// Error logs a message at ERROR level.
func Error(format string, args ...interface{}) {
	logMessage(ERROR, "ERROR", format, args...)
}

// Fatal logs a message at ERROR level and terminates the process with os.Exit(1).
func Fatal(format string, args ...interface{}) {
	logMessage(ERROR, "FATAL", format, args...)
	os.Exit(1)
}

// DebugComponent logs a message at DEBUG level for a specific component.
func DebugComponent(comp Component, format string, args ...interface{}) {
	logMessageWithComponent(DEBUG, comp, "DEBUG", format, args...)
}

// InfoComponent logs a message at INFO level for a specific component.
func InfoComponent(comp Component, format string, args ...interface{}) {
	logMessageWithComponent(INFO, comp, "INFO", format, args...)
}

// WarnComponent logs a message at WARN level for a specific component.
func WarnComponent(comp Component, format string, args ...interface{}) {
	logMessageWithComponent(WARN, comp, "WARN", format, args...)
}

// ErrorComponent logs a message at ERROR level for a specific component.
func ErrorComponent(comp Component, format string, args ...interface{}) {
	logMessageWithComponent(ERROR, comp, "ERROR", format, args...)
}

// ErrorfWithContext wraps an error with additional context, producing
// a formatted error like "context: original error".
func ErrorfWithContext(err error, format string, args ...interface{}) error {
	context := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s: %w", context, err)
}
