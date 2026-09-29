// Package logger provides a process-wide JSON logger built on log/slog.
// Every line has the form:
//
//	{"timestamp":1727568000000,"level":"INFO","msg":"...","component":"my-service",...}
//
// timestamp is Unix epoch milliseconds, the format New Relic reads natively.
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

// TimestampKey is the key that holds the log time, in Unix epoch milliseconds.
const TimestampKey = "timestamp"

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
		h := slog.NewJSONHandler(out, &slog.HandlerOptions{ReplaceAttr: epochMillis})
		std = slog.New(h).With(ComponentKey, component)
		slog.SetDefault(std)
		log.SetFlags(0)
	})
}

// epochMillis replaces slog's RFC 3339 time attr with TimestampKey in
// Unix epoch milliseconds.
func epochMillis(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Int64(TimestampKey, a.Value.Time().UnixMilli())
	}
	return a
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
