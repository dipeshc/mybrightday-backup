package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// plainHandler is a slog.Handler that writes only the message to the writer,
// with no timestamp or level prefix, for clean human-readable console output.
type plainHandler struct {
	w     io.Writer
	level slog.Level
}

func (h *plainHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }
func (h *plainHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *plainHandler) WithGroup(_ string) slog.Handler              { return h }
func (h *plainHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteByte(' ')
		b.WriteString(a.Key)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", a.Value.Any())
		return true
	})
	fmt.Fprintln(h.w, b.String())
	return nil
}

// SetupLogging initializes the global slog logger.
func SetupLogging(verbose bool, format string) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	default:
		handler = &plainHandler{w: os.Stderr, level: level}
	}
	slog.SetDefault(slog.New(handler))
}
