package clog

import (
	"context"
	"log/slog"
)

type AttributesHandler struct {
	handler slog.Handler
}

func NewAttributesHandler(handler slog.Handler) *AttributesHandler {
	return &AttributesHandler{
		handler: handler,
	}
}

func (h *AttributesHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *AttributesHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := GetAttributes(ctx)
	if len(attrs) > 0 {
		record.AddAttrs(mapToAttrs(attrs)...)
	}
	return h.handler.Handle(ctx, record)
}

func (h *AttributesHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &AttributesHandler{
		handler: h.handler.WithAttrs(attrs),
	}
}

func (h *AttributesHandler) WithGroup(name string) slog.Handler {
	return &AttributesHandler{
		handler: h.handler.WithGroup(name),
	}
}

func mapToAttrs(m map[string]any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(m))
	for k, v := range m {
		attrs = append(attrs, slog.Any(k, v))
	}
	return attrs
}
