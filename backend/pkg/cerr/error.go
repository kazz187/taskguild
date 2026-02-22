package cerr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/kazz187/taskguild/backend/pkg/clog"
)

type Error struct {
	Code    Code
	Msg     string          // ユーザーへ Code とともに返却するメッセージ
	Err     error           // ログに残したいエラー
	Stack   string          // スタックトレース
	Details []proto.Message // ユーザーへ返却したい詳細なエラー
}

func NewError(code Code, msg string, underlying error) *Error {
	err := &Error{
		Code: code,
		Msg:  msg,
		Err:  underlying,
	}
	if clog.ConnectCodeToLevel(code.ConnectCode()) == clog.LevelError {
		stackTrace := make([]byte, 2048)
		n := runtime.Stack(stackTrace, false)
		err.Stack = string(stackTrace[0:n])
	}
	return err
}

func NewErrorWithDetails(code Code, msg string, underlying error, details []proto.Message) *Error {
	err := NewError(code, msg, underlying)
	err.Details = details
	return err
}

func (e *Error) AddDetailError(err proto.Message) {
	e.Details = append(e.Details, err)
}

func (e *Error) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("[%s] %s", e.Code.String(), e.Msg)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Code.String(), e.Msg, e.Err.Error())
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) AddDetailMessage(msg string) error {
	protoMsg := validate.Violation{
		Message: &msg,
	}
	e.Details = append(e.Details, &protoMsg)
	return e
}

func (e *Error) AddDetailMessageWithCode(msg string, code string) error {
	protoMsg := validate.Violation{
		Message: &msg,
		RuleId:  &code,
	}
	e.Details = append(e.Details, &protoMsg)
	return e
}

func (e *Error) ConnectError() *connect.Error {
	connectErr := connect.NewError(e.Code.ConnectCode(), errors.New(e.Msg))
	for _, detailMsg := range e.Details {
		detail, err := connect.NewErrorDetail(detailMsg)
		if err != nil {
			continue
		}
		connectErr.AddDetail(detail)
	}
	return connectErr
}

func ExtractConnectError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return NewError(Canceled, "connection closed", err).ConnectError()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.Err == "operation was canceled" {
		return NewError(Canceled, "connection closed", err).ConnectError()
	}

	clog.AddError(ctx, err)
	var cerr *Error
	if errors.As(err, &cerr) {
		if cerr.Stack != "" {
			clog.AddStack(ctx, cerr.Stack)
		}
		return cerr.ConnectError()
	}
	return NewError(Unknown, "unknown error", err).ConnectError()
}

type httpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ExtractToHTTPResponse(ctx context.Context, rw http.ResponseWriter, response *responseReceiver) {
	if response.err == nil {
		writeJSON(ctx, rw, response.response)
		return
	}
	if errors.Is(response.err, context.Canceled) {
		writeJSONError(ctx, rw, NewError(Canceled, "connection closed", response.err))
		return
	}
	var dnsErr *net.DNSError
	if errors.As(response.err, &dnsErr) && dnsErr.Err == "operation was canceled" {
		writeJSONError(ctx, rw, NewError(Canceled, "connection closed", response.err))
		return
	}

	clog.AddError(ctx, response.err)
	var cErr *Error
	if errors.As(response.err, &cErr) {
		if cErr.Stack != "" {
			clog.AddStack(ctx, cErr.Stack)
		}
		writeJSONError(ctx, rw, cErr)
		return
	}
	writeJSONError(ctx, rw, NewError(Unknown, "unknown error", response.err))
}

func writeJSON(ctx context.Context, rw http.ResponseWriter, response any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(response); err != nil {
		writeJSONError(ctx, rw, NewError(Internal, "server error", err))
	}
	if _, err := rw.Write(buf.Bytes()); err != nil {
		clog.AddError(ctx, NewError(Internal, "server error", err))
	}
	rw.WriteHeader(http.StatusOK)
}

func writeJSONError(ctx context.Context, rw http.ResponseWriter, origErr *Error) {
	rw.WriteHeader(origErr.Code.HTTPCode())
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(httpError{Code: origErr.Code.String(), Message: origErr.Msg}); err != nil {
		buf = bytes.NewBufferString(`{"code":"internal","message":"server error"}`)
		origErr.Err = errors.Join(origErr.Err, err)
		clog.AddError(ctx, origErr)
	}
	if _, err := rw.Write(buf.Bytes()); err != nil {
		origErr.Err = errors.Join(origErr.Err, err)
		clog.AddError(ctx, origErr)
	}
}

func IsCode(err error, code Code) bool {
	var cerr *Error
	if errors.As(err, &cerr) {
		return cerr.Code == code
	}
	return false
}
