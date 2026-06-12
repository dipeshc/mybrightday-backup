package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestPlainHandlerHandle(t *testing.T) {
	var buf bytes.Buffer
	h := &plainHandler{w: &buf, level: slog.LevelInfo}

	r := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)
	r.AddAttrs(slog.String("key", "value"), slog.Int("count", 3))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := buf.String(); got != "hello key=value count=3\n" {
		t.Errorf("output = %q, want %q", got, "hello key=value count=3\n")
	}
}

func TestPlainHandlerHandleNoAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := &plainHandler{w: &buf, level: slog.LevelInfo}

	r := slog.Record{Message: "bare", Level: slog.LevelInfo}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := buf.String(); got != "bare\n" {
		t.Errorf("output = %q, want %q", got, "bare\n")
	}
}

func TestPlainHandlerEnabled(t *testing.T) {
	h := &plainHandler{level: slog.LevelInfo}
	tests := []struct {
		level slog.Level
		want  bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo, true},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}
	for _, tt := range tests {
		if got := h.Enabled(context.Background(), tt.level); got != tt.want {
			t.Errorf("Enabled(%v) = %t, want %t", tt.level, got, tt.want)
		}
	}
}

func TestPlainHandlerWithAttrsAndGroup(t *testing.T) {
	h := &plainHandler{level: slog.LevelInfo}
	if h.WithAttrs(nil) != slog.Handler(h) {
		t.Error("WithAttrs should return the handler itself")
	}
	if h.WithGroup("g") != slog.Handler(h) {
		t.Error("WithGroup should return the handler itself")
	}
}

func TestSetup(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })

	tests := []struct {
		name         string
		cfg          Config
		debugEnabled bool
		infoEnabled  bool
	}{
		{"defaults", Config{}, false, true},
		{"debug level", Config{Level: "debug"}, true, true},
		{"warn level", Config{Level: "WARN"}, false, false},
		{"error level", Config{Level: "Error"}, false, false},
		{"explicit info", Config{Level: "INFO"}, false, true},
		{"unknown level falls back to info", Config{Level: "loud"}, false, true},
		{"json format", Config{Format: "json", Level: "DEBUG"}, true, true},
		{"text-full format", Config{Format: "text-full"}, false, true},
		{"text-simple format", Config{Format: "text-simple"}, false, true},
		{"unknown format falls back to plain", Config{Format: "xml"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Setup(tt.cfg)
			logger := slog.Default()
			ctx := context.Background()
			if got := logger.Enabled(ctx, slog.LevelDebug); got != tt.debugEnabled {
				t.Errorf("debug enabled = %t, want %t", got, tt.debugEnabled)
			}
			if got := logger.Enabled(ctx, slog.LevelInfo); got != tt.infoEnabled {
				t.Errorf("info enabled = %t, want %t", got, tt.infoEnabled)
			}
		})
	}
}

// Guard against the handler writing anything beyond the message line, like
// timestamps or level prefixes.
func TestPlainHandlerOutputHasNoPrefix(t *testing.T) {
	var buf bytes.Buffer
	h := &plainHandler{w: &buf, level: slog.LevelInfo}
	logger := slog.New(h)
	logger.Info("clean message", "a", 1)

	out := buf.String()
	if !strings.HasPrefix(out, "clean message") {
		t.Errorf("output %q should start with the message", out)
	}
}
