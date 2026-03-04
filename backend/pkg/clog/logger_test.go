package clog

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestContextWithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)).With("test_key", "test_value")
	ctx := ContextWithLogger(context.Background(), logger)
	got := LoggerFromContext(ctx)
	if got != logger {
		t.Error("expected same logger instance from context")
	}
}

func TestLoggerFromContext_Default(t *testing.T) {
	got := LoggerFromContext(context.Background())
	if got != slog.Default() {
		t.Error("expected slog.Default() when no logger in context")
	}
}
