package cerr

import (
	"context"
	"net/http"
)

type responseReceiverKey struct{}

type responseReceiver struct {
	response any
	err      error
}

func contextWithResponseReceiver(ctx context.Context, err *responseReceiver) context.Context {
	return context.WithValue(ctx, responseReceiverKey{}, err)
}

func responseReceiverFromContext(ctx context.Context) *responseReceiver {
	if err, ok := ctx.Value(responseReceiverKey{}).(*responseReceiver); ok {
		return err
	}
	return nil
}

func SetJSONResponse(ctx context.Context, response any) {
	if rr := responseReceiverFromContext(ctx); rr != nil {
		rr.response = response
	}
}

func SetJSONError(ctx context.Context, err error) {
	if rr := responseReceiverFromContext(ctx); rr != nil {
		rr.err = err
	}
}

func SetNewJSONError(ctx context.Context, code Code, msg string, err error) {
	SetJSONError(ctx, NewError(code, msg, err))
}

func NewConvertConnectErrorChiMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rr := &responseReceiver{}
			ctx := contextWithResponseReceiver(r.Context(), rr)
			next.ServeHTTP(rw, r.WithContext(ctx))
			ExtractToHTTPResponse(ctx, rw, rr)
		})
	}
}
