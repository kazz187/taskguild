package clog

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

type connectConfig struct {
	Filter func(spec connect.Spec) bool
}

type ConnectOption interface {
	apply(*connectConfig)
}

type connectOptionFunc func(*connectConfig)

func (o connectOptionFunc) apply(c *connectConfig) {
	o(c)
}

func WithConnectFilter(filter func(connect.Spec) bool) ConnectOption {
	return connectOptionFunc(func(cfg *connectConfig) {
		cfg.Filter = filter
	})
}

func DefaultConnectHealthCheckUnaryFilter(spec connect.Spec) bool {
	return spec.Procedure != "/grpc.health.v1.Health/Check"
}

func NewSlogConnectUnaryInterceptor(opts ...ConnectOption) connect.UnaryInterceptorFunc {
	cfg := connectConfig{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			startTime := time.Now()
			newCtx := ContextWithSlog(ctx)

			AddAttributes(newCtx, map[string]any{
				"method":            req.HTTPMethod(),
				"procedure":         req.Spec().Procedure,
				"stream_type":       req.Spec().StreamType.String(),
				"idempotency_level": req.Spec().IdempotencyLevel.String(),
			})
			resp, err := next(newCtx, req)
			if cfg.Filter != nil && !cfg.Filter(req.Spec()) {
				return resp, err
			}
			codeStr := "ok"
			var cerr *connect.Error
			if err != nil {
				if !errors.As(err, &cerr) {
					cerr = connect.NewError(connect.CodeUnknown, err)
				}
				codeStr = cerr.Code().String()
			}
			AddAttributes(newCtx, map[string]any{
				"code":     codeStr,
				"duration": time.Since(startTime),
			})

			if cerr == nil {
				slog.InfoContext(newCtx, "Finished")
			} else {
				logConnectError(newCtx, cerr)
			}
			return resp, err
		}
	}
}

type slogConnectInterceptor struct {
	cfg   connectConfig
	unary connect.UnaryInterceptorFunc
}

func (s *slogConnectInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return s.unary(next)
}

func (s *slogConnectInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (s *slogConnectInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		startTime := time.Now()
		newCtx := ContextWithSlog(ctx)

		AddAttributes(newCtx, map[string]any{
			"procedure":         conn.Spec().Procedure,
			"stream_type":       conn.Spec().StreamType.String(),
			"idempotency_level": conn.Spec().IdempotencyLevel.String(),
		})
		slog.InfoContext(newCtx, "Connected")

		err := next(newCtx, conn)

		if s.cfg.Filter != nil && !s.cfg.Filter(conn.Spec()) {
			return err
		}
		codeStr := "ok"
		var cerr *connect.Error
		if err != nil {
			if errors.As(err, &cerr) {
				codeStr = cerr.Code().String()
			} else {
				cerr = connect.NewError(connect.CodeUnknown, err)
				codeStr = cerr.Code().String()
			}
		}
		AddAttributes(newCtx, map[string]any{
			"code":     codeStr,
			"duration": time.Since(startTime),
		})
		if cerr == nil {
			slog.InfoContext(newCtx, "Finished")
		} else {
			logConnectError(newCtx, cerr)
		}
		return err
	}
}

func NewSlogConnectInterceptor(opts ...ConnectOption) connect.Interceptor {
	cfg := connectConfig{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	return &slogConnectInterceptor{
		cfg:   cfg,
		unary: NewSlogConnectUnaryInterceptor(opts...),
	}
}

func logConnectError(ctx context.Context, cerr *connect.Error) {
	if errDetails := cerr.Details(); len(errDetails) > 0 {
		details := make([]proto.Message, 0, len(errDetails))
		for _, detail := range errDetails {
			val, err := detail.Value()
			if err != nil {
				slog.ErrorContext(ctx, "failed to convert detail value", ErrorAttributeKey, err)
				continue
			}
			details = append(details, val)
		}
		AddAttribute(ctx, "err_details", details)
	}

	switch ConnectCodeToLevel(cerr.Code()) {
	case LevelError:
		slog.ErrorContext(ctx, cerr.Message())
	case LevelWarn:
		slog.WarnContext(ctx, cerr.Message())
	case LevelInfo:
		slog.InfoContext(ctx, cerr.Message())
	case LevelDebug:
		slog.DebugContext(ctx, cerr.Message())
	}
}
