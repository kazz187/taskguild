package clog

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
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

func NewSlogConnectServerInterceptor(opts ...ConnectOption) connect.ServerInterceptor {
	cfg := connectConfig{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			startTime := time.Now()
			newCtx := ContextWithSlog(ctx)

			AddAttributes(newCtx, map[string]any{
				ProcedureAttributeKey:        spec.Procedure,
				StreamTypeAttributeKey:       spec.StreamType.String(),
				IdempotencyLevelAttributeKey: spec.IdempotencyLevel.String(),
			})

			if httpInfo, ok := connecthttp.ServerInfoForContext(ctx); ok {
				AddAttribute(newCtx, MethodAttributeKey, httpInfo.HTTPMethod())
			}

			// Filter にかかった場合アクセスログの出力をスキップする
			logAccess := cfg.Filter == nil || cfg.Filter(spec)
			if logAccess && spec.StreamType != connect.StreamTypeUnary {
				slog.InfoContext(newCtx, "Connected")
			}

			err := next(newCtx, spec, stream)
			if logAccess {
				logConnectResult(newCtx, startTime, err)
			}

			return err
		}
	}
}

func logConnectResult(ctx context.Context, startTime time.Time, err error) {
	codeStr := "ok"

	var cerr *connect.Error
	if err != nil {
		if !errors.As(err, &cerr) {
			code := connect.CodeUnknown

			switch {
			case errors.Is(err, context.Canceled):
				code = connect.CodeCanceled
			case errors.Is(err, context.DeadlineExceeded):
				code = connect.CodeDeadlineExceeded
			}

			cerr = connect.NewError(code, err.Error())
		}

		codeStr = cerr.Code().String()
	}

	AddAttributes(ctx, map[string]any{
		CodeAttributeKey:     codeStr,
		DurationAttributeKey: time.Since(startTime),
	})

	if cerr == nil {
		slog.InfoContext(ctx, "Finished")
		return
	}

	if errDetails := cerr.Details(); len(errDetails) > 0 {
		details := make([]proto.Message, 0, len(errDetails))
		for _, detail := range errDetails {
			val, err := connectproto.UnmarshalErrorDetail(detail)
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
