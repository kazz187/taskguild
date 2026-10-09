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
	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectproto"
	"google.golang.org/protobuf/proto"

	"github.com/kazz187/taskguild/pkg/clog"
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
	connectErr := connect.NewError(e.Code.ConnectCode(), e.Msg)
	for _, detailMsg := range e.Details {
		detail, err := connectproto.NewErrorDetail(detailMsg)
		if err != nil {
			continue
		}

		connectErr = connectErr.WithDetail(detail)
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
		// 発生箇所のスタックがあれば ctx に保存
		if stack := innermostStack(err); stack != "" {
			clog.AddStack(ctx, stack)
		}

		return cerr.ConnectError()
	}

	// connect.Error の場合はそのまま返す
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		// 下流の RPC から受け取ったエラーは Internal として扱う
		if connectErr.IsRemote() {
			return NewError(Internal, "server error", err).ConnectError()
		}

		return connectErr
	}

	return NewError(Unknown, "unknown error", err).ConnectError()
}

// innermostStack はエラーチェーンをたどり、スタックトレースを持つ *Error のうち
// 最も内側 (= 原因の発生箇所に最も近い) ものの Stack を返す。無ければ空文字列。
//
// 上位で cerr を包み直すと、クライアントへ返すコードとメッセージは最外の *Error で
// 決まるが、ログに残すスタックは発生箇所のものにしたい。外側がスタックを取らない
// コード (NotFound 等) で内側がスタックを取るコード (Internal 等) のときも、発生箇所の
// スタックが残る。Unwrap() error と Unwrap() []error (errors.Join) の両方をたどる。
func innermostStack(err error) string {
	stack, _ := deepestStack(err, 0)

	return stack
}

// deepestStack は err 以下で Stack を持つ最も深い *Error の Stack とその深さを返す
// (見つからなければ深さ -1)。
func deepestStack(err error, depth int) (string, int) {
	if err == nil {
		return "", -1
	}

	// 深さを数えるため、errors.As のようにチェーン全体ではなくこのノードだけを見る
	node := any(err)

	stack, stackDepth := "", -1

	e, ok := node.(*Error)
	if ok && e.Stack != "" {
		stack, stackDepth = e.Stack, depth
	}

	var children []error

	switch u := node.(type) {
	case interface{ Unwrap() error }:
		children = []error{u.Unwrap()}
	case interface{ Unwrap() []error }:
		children = u.Unwrap()
	}

	for _, child := range children {
		if s, d := deepestStack(child, depth+1); d > stackDepth {
			stack, stackDepth = s, d
		}
	}

	return stack, stackDepth
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
		// 発生箇所のスタックがあれば ctx に保存
		if stack := innermostStack(response.err); stack != "" {
			clog.AddStack(ctx, stack)
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

	err := enc.Encode(response)
	if err != nil {
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

	err := enc.Encode(httpError{Code: origErr.Code.String(), Message: origErr.Msg})
	if err != nil {
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
