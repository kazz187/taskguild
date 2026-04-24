package clog

import (
	"context"
	"log/slog"
)

type ctxLoggerKey struct{}

// ContextWithLogger returns a new context carrying the given *slog.Logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxLoggerKey{}, logger)
}

// LoggerFromContext retrieves the *slog.Logger from context.
// If none is set, returns slog.Default().
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxLoggerKey{}).(*slog.Logger); ok {
		return l
	}

	return slog.Default()
}
