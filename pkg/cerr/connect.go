package cerr

import (
	"context"

	"connectrpc.com/connect"
)

type convertConnectErrorInterceptor struct{}

func NewConvertConnectErrorInterceptor() connect.Interceptor {
	return &convertConnectErrorInterceptor{}
}

func (i *convertConnectErrorInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)
		return resp, ExtractConnectError(ctx, err)
	}
}

func (i *convertConnectErrorInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *convertConnectErrorInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)
		return ExtractConnectError(ctx, err)
	}
}
