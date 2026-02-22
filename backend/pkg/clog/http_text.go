package clog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/fatih/color"
)

type HTTPTextHandler struct {
	cfg    TextHandlerConfig
	groups []string
	attrs  []slog.Attr
	w      io.Writer
}

func (h *HTTPTextHandler) clone() *HTTPTextHandler {
	nh := *h
	nh.groups = make([]string, len(h.groups))
	copy(nh.groups, h.groups)
	nh.attrs = make([]slog.Attr, len(h.attrs))
	copy(nh.attrs, h.attrs)
	return &nh
}

func (h *HTTPTextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.cfg.Level != nil {
		minLevel = h.cfg.Level.Level()
	}
	return l >= minLevel
}

func (h *HTTPTextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

type TextHandlerConfig struct {
	Color bool
	Level *slog.Level
}

type TextHandlerOption func(*TextHandlerConfig)

func WithColor(c bool) TextHandlerOption {
	return func(cfg *TextHandlerConfig) {
		cfg.Color = c
	}
}

func WithLevel(level slog.Level) TextHandlerOption {
	return func(cfg *TextHandlerConfig) {
		cfg.Level = &level
	}
}

func NewHTTPTextHandler(w io.Writer, opts ...TextHandlerOption) *HTTPTextHandler {
	cfg := TextHandlerConfig{
		Color: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &HTTPTextHandler{
		cfg: cfg,
		w:   w,
	}
}

func (h *HTTPTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := h.clone()
	nh.attrs = append(nh.attrs, attrs...)
	return nh
}

func (h *HTTPTextHandler) Handle(ctx context.Context, record slog.Record) error {
	color.NoColor = !h.cfg.Color
	color.Output = h.w

	c := color.New()
	defer color.Unset()
	if _, err := c.Printf("%s ", record.Time.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("can't write time: %w", err)
	}
	switch record.Level {
	case slog.LevelDebug:
		c = color.Set(color.FgCyan)
	case slog.LevelInfo:
		c = color.Set(color.FgBlue)
	case slog.LevelWarn:
		c = color.Set(color.FgYellow)
	case slog.LevelError:
		c = color.Set(color.FgRed)
	default:
	}
	if _, err := c.Printf("%s ", record.Level); err != nil {
		return fmt.Errorf("can't write Level: %w", err)
	}

	c = color.New()
	kv := map[string]slog.Value{}
	for _, attr := range h.attrs {
		kv[attr.Key] = attr.Value
	}
	record.Attrs(func(attr slog.Attr) bool {
		kv[attr.Key] = attr.Value
		return true
	})
	for _, key := range []string{"proto", "method", "path", "status"} {
		if err := printColumn(c, kv, key); err != nil {
			return err
		}
	}

	c = color.Set(color.FgGreen)
	if _, err := c.Printf("%s", record.Message); err != nil {
		return fmt.Errorf("can't write newline: %w", err)
	}
	if e, ok := kv[ErrorAttributeKey]; ok {
		delete(kv, ErrorAttributeKey)
		c = color.Set(color.FgRed)
		if _, err := c.Printf(" %s ", e); err != nil {
			return fmt.Errorf("can't write err: %w", err)
		}
	}
	if _, err := c.Printf("\n"); err != nil {
		return err
	}

	c = color.New()
	var keys []string
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := c.Printf("    %s=%s\n", k, kv[k]); err != nil {
			return fmt.Errorf("can't write %s: %w", k, err)
		}
	}
	return nil
}

func printColumn(c *color.Color, kv map[string]slog.Value, key string) error {
	if v, ok := kv[key]; ok {
		if _, err := c.Printf("%s ", v); err != nil {
			return fmt.Errorf("can't write %s: %w", key, err)
		}
		delete(kv, key)
	}
	return nil
}
