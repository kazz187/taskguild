package clog

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type chiConfig struct {
	Filter func(r *http.Request) bool
}

type ChiOption interface {
	apply(*chiConfig)
}

type chiOptionFunc func(*chiConfig)

func (o chiOptionFunc) apply(c *chiConfig) {
	o(c)
}

func WithChiFilter(filter func(r *http.Request) bool) ChiOption {
	return chiOptionFunc(func(cfg *chiConfig) {
		cfg.Filter = filter
	})
}

func SlogChiMiddleware(opts ...ChiOption) func(http.Handler) http.Handler {
	cfg := chiConfig{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			ctx := ContextWithSlog(r.Context())
			AddAttributes(ctx, map[string]any{
				"method":    r.Method,
				"procedure": r.URL.Path,
				"proto":     r.Proto,
			})
			next.ServeHTTP(ww, r.WithContext(ctx))
			if cfg.Filter != nil && !cfg.Filter(r) {
				return
			}
			AddAttributes(ctx, map[string]any{
				"status":        ww.Status(),
				"bytes_written": ww.BytesWritten(),
				"duration":      time.Since(startTime),
			})
			msg := http.StatusText(ww.Status())
			switch HTTPStatusToLevel(ww.Status()) {
			case LevelError:
				slog.ErrorContext(ctx, msg)
			case LevelWarn:
				slog.WarnContext(ctx, msg)
			case LevelInfo:
				slog.InfoContext(ctx, msg)
			case LevelDebug:
				slog.DebugContext(ctx, msg)
			}
		})
	}
}
