package cerr

import (
	"context"

	"connectrpc.com/connect/v2"
)

func NewConvertConnectErrorServerInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			return ExtractConnectError(ctx, next(ctx, spec, stream))
		}
	}
}
