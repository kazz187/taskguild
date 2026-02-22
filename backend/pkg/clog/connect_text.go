package clog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/fatih/color"
)

type ConnectTextHandler struct {
	cfg    TextHandlerConfig
	groups []string
	attrs  []slog.Attr
	w      io.Writer
}

func (h *ConnectTextHandler) clone() *ConnectTextHandler {
	nh := *h
	nh.groups = make([]string, len(h.groups))
	copy(nh.groups, h.groups)
	nh.attrs = make([]slog.Attr, len(h.attrs))
	copy(nh.attrs, h.attrs)
	return &nh
}

func (h *ConnectTextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.cfg.Level != nil {
		minLevel = h.cfg.Level.Level()
	}
	return l >= minLevel
}

func (h *ConnectTextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

func NewConnectTextHandler(w io.Writer, opts ...TextHandlerOption) *ConnectTextHandler {
	cfg := TextHandlerConfig{
		Color: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &ConnectTextHandler{
		cfg: cfg,
		w:   w,
	}
}

func (h *ConnectTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := h.clone()
	nh.attrs = append(nh.attrs, attrs...)
	return nh
}

func (h *ConnectTextHandler) Handle(ctx context.Context, record slog.Record) error {
	color.NoColor = !h.cfg.Color
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	color.Output = buf

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
	for _, key := range []string{"method", "stream_type", "procedure"} {
		if err := printColumn(c, kv, key); err != nil {
			return err
		}
	}

	c = color.Set(color.FgGreen)
	if _, err := c.Printf("\""); err != nil {
		return fmt.Errorf("can't write quote: %w", err)
	}
	if v, ok := kv["code"]; ok {
		delete(kv, "code")
		if _, err := c.Printf("[%s] ", v); err != nil {
			return fmt.Errorf("can't write code: %w", err)
		}
	}
	if _, err := c.Printf("%s\"", record.Message); err != nil {
		return fmt.Errorf("can't write newline: %w", err)
	}
	if e, ok := kv[ErrorAttributeKey]; ok {
		delete(kv, ErrorAttributeKey)
		c = color.Set(color.FgRed)
		if _, err := c.Printf(" \"%s\"", e); err != nil {
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
	if _, err := h.w.Write(buf.Bytes()); err != nil {
		return err
	}

	return nil
}
