// Package logger provides a process-wide JSON logger built on log/slog.
// Every line has the form:
//
//	{"time":"...","level":"INFO","msg":"...","component":"my-service",...}
//
// Call Init once in main, then log with Info, Warn and Error.
package logger

import (
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
)

// ComponentKey is the key that holds the service name on every line.
const ComponentKey = "component"

// defaultComponent is used when logging happens before Init.
const defaultComponent = "unknown"

var (
	once sync.Once
	std  *slog.Logger
	out  io.Writer = os.Stdout
)

// Init sets up the logger, tagging every line with component. It also
// routes the standard log package through it. Only the first call takes
// effect, including the implicit one made by logging before Init.
func Init(component string) {
	once.Do(func() {
		h := slog.NewJSONHandler(out, nil)
		std = slog.New(h).With(ComponentKey, component)
		slog.SetDefault(std)
		log.SetFlags(0)
	})
}

// get returns the logger, initialising it with defaultComponent if needed.
func get() *slog.Logger {
	Init(defaultComponent)
	return std
}

// Info logs msg with optional key/value pairs, e.g.
// Info("request handled", "path", path, "duration_ms", 12).
func Info(msg string, args ...any) { get().Info(msg, args...) }

// Warn logs msg at warn level; see Info for args.
func Warn(msg string, args ...any) { get().Warn(msg, args...) }

// Error logs msg at error level; see Info for args. Pass errors as an
// "error" arg.
func Error(msg string, args ...any) { get().Error(msg, args...) }
