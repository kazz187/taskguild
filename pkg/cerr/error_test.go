package cerr_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect/v2"

	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/clog"
)

var (
	errPlain = errors.New("plain")
	errDial  = errors.New("dial tcp: refused")
)

func TestInnermostStack(t *testing.T) {
	t.Parallel()

	inner := &cerr.Error{Code: cerr.Internal, Msg: "inner", Stack: "inner stack"}
	middle := &cerr.Error{Code: cerr.Internal, Msg: "middle", Err: inner, Stack: "middle stack"}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "single error with stack",
			err:  inner,
			want: "inner stack",
		},
		{
			name: "outer without stack wraps inner with stack",
			err:  &cerr.Error{Code: cerr.NotFound, Msg: "outer", Err: inner},
			want: "inner stack",
		},
		{
			name: "both have stacks: innermost wins",
			err:  &cerr.Error{Code: cerr.Internal, Msg: "outer", Err: middle, Stack: "outer stack"},
			want: "inner stack",
		},
		{
			name: "through fmt.Errorf wrapping",
			err:  &cerr.Error{Code: cerr.NotFound, Msg: "outer", Err: fmt.Errorf("context: %w", middle)},
			want: "inner stack",
		},
		{
			name: "through errors.Join: deepest branch wins",
			err: &cerr.Error{Code: cerr.Internal, Msg: "outer", Stack: "outer stack", Err: errors.Join(
				&cerr.Error{Code: cerr.Internal, Msg: "shallow", Stack: "shallow stack"},
				fmt.Errorf("context: %w", inner),
			)},
			want: "inner stack",
		},
		{
			name: "inner without stack keeps the nearest stack",
			err: &cerr.Error{
				Code: cerr.Internal, Msg: "outer", Stack: "outer stack",
				Err: &cerr.Error{Code: cerr.NotFound, Msg: "inner"},
			},
			want: "outer stack",
		},
		{
			name: "no stack anywhere",
			err:  &cerr.Error{Code: cerr.NotFound, Msg: "outer", Err: errPlain},
			want: "",
		},
		{
			name: "plain error",
			err:  errPlain,
			want: "",
		},
		{
			name: "nil",
			err:  nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := cerr.InnermostStack(tt.err); got != tt.want {
				t.Errorf("InnermostStack() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractConnectError_StackFromInnermostAndCodeFromOutermost(t *testing.T) {
	t.Parallel()

	inner := &cerr.Error{Code: cerr.Internal, Msg: "db failure", Err: errDial, Stack: "inner stack"}
	outer := cerr.NewError(cerr.NotFound, "project not found", inner)

	ctx := clog.ContextWithSlog(context.Background())
	got := cerr.ExtractConnectError(ctx, outer)

	var connectErr *connect.Error
	if !errors.As(got, &connectErr) {
		t.Fatalf("ExtractConnectError() = %v, want *connect.Error", got)
	}

	if connectErr.Code() != connect.CodeNotFound {
		t.Errorf("code = %v, want %v (the outermost cerr decides the code)", connectErr.Code(), connect.CodeNotFound)
	}

	if connectErr.Message() != "project not found" {
		t.Errorf("message = %q, want %q (the outermost cerr decides the message)", connectErr.Message(), "project not found")
	}

	if stack := clog.GetStack(ctx); stack != "inner stack" {
		t.Errorf("logged stack = %q, want %q (the innermost cerr's stack)", stack, "inner stack")
	}

	if !errors.Is(clog.GetError(ctx), inner) {
		t.Errorf("logged error = %v, want the whole chain including the cause", clog.GetError(ctx))
	}
}

// Handlers used to return cerr.NewError(...).ConnectError(), and the
// interceptor then turned that *connect.Error into unknown.
func TestExtractConnectError_ConvertedErrorKeepsCode(t *testing.T) {
	t.Parallel()

	converted := cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()

	got := cerr.ExtractConnectError(clog.ContextWithSlog(context.Background()), converted)
	if !errors.Is(got, converted) {
		t.Errorf("ExtractConnectError() = %v, want %v unchanged", got, converted)
	}
}

func TestExtractConnectError_RemoteErrorBecomesInternal(t *testing.T) {
	t.Parallel()

	remote := connect.NewError(connect.CodeNotFound, "item not found").WithRemote()

	ctx := clog.ContextWithSlog(context.Background())
	got := cerr.ExtractConnectError(ctx, fmt.Errorf("downstream: %w", remote))

	var connectErr *connect.Error
	if !errors.As(got, &connectErr) {
		t.Fatalf("ExtractConnectError() = %v, want *connect.Error", got)
	}

	if connectErr.Code() != connect.CodeInternal {
		t.Errorf("code = %v, want %v", connectErr.Code(), connect.CodeInternal)
	}

	if connectErr.IsRemote() {
		t.Error("IsRemote() = true, want false")
	}

	if !errors.Is(clog.GetError(ctx), remote) {
		t.Errorf("logged error = %v, want the remote error", clog.GetError(ctx))
	}
}

func TestExtractConnectError_LocalConnectErrorPassesThrough(t *testing.T) {
	t.Parallel()

	local := connect.NewError(connect.CodeInvalidArgument, "invalid")

	got := cerr.ExtractConnectError(clog.ContextWithSlog(context.Background()), local)
	if !errors.Is(got, local) {
		t.Errorf("ExtractConnectError() = %v, want %v unchanged", got, local)
	}
}

func TestExtractConnectError_PlainErrorBecomesUnknown(t *testing.T) {
	t.Parallel()

	got := cerr.ExtractConnectError(clog.ContextWithSlog(context.Background()), errPlain)
	if code := connect.CodeOf(got); code != connect.CodeUnknown {
		t.Errorf("code = %v, want %v", code, connect.CodeUnknown)
	}
}

func TestExtractConnectError_NoStackIsNotAdded(t *testing.T) {
	t.Parallel()

	ctx := clog.ContextWithSlog(context.Background())

	got := cerr.ExtractConnectError(ctx, cerr.NewError(cerr.NotFound, "project not found", nil))
	if got == nil {
		t.Fatal("ExtractConnectError() = nil, want an error")
	}

	if stack := clog.GetStack(ctx); stack != "" {
		t.Errorf("logged stack = %q, want empty", stack)
	}
}
