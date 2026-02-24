package cerr

import (
	"net/http"

	"connectrpc.com/connect"
)

//go:generate go tool stringer -type=Code -output=code_string.go code.go
type Code int

const (
	OK                 = Code(0)
	Canceled           = Code(1)
	Unknown            = Code(2)
	InvalidArgument    = Code(3)
	DeadlineExceeded   = Code(4)
	NotFound           = Code(5)
	AlreadyExists      = Code(6)
	PermissionDenied   = Code(7)
	ResourceExhausted  = Code(8)
	FailedPrecondition = Code(9)
	Aborted            = Code(10)
	OutOfRange         = Code(11)
	Unimplemented      = Code(12)
	Internal           = Code(13)
	Unavailable        = Code(14)
	DataLoss           = Code(15)
	Unauthenticated    = Code(16)
)

var codeToConnectCodeMap = map[Code]connect.Code{
	Canceled:           connect.CodeCanceled,
	Unknown:            connect.CodeUnknown,
	InvalidArgument:    connect.CodeInvalidArgument,
	DeadlineExceeded:   connect.CodeDeadlineExceeded,
	NotFound:           connect.CodeNotFound,
	AlreadyExists:      connect.CodeAlreadyExists,
	PermissionDenied:   connect.CodePermissionDenied,
	ResourceExhausted:  connect.CodeResourceExhausted,
	FailedPrecondition: connect.CodeFailedPrecondition,
	Aborted:            connect.CodeAborted,
	OutOfRange:         connect.CodeOutOfRange,
	Unimplemented:      connect.CodeUnimplemented,
	Internal:           connect.CodeInternal,
	Unavailable:        connect.CodeUnavailable,
	DataLoss:           connect.CodeDataLoss,
	Unauthenticated:    connect.CodeUnauthenticated,
}

var connectCodeToCodeMap = map[connect.Code]Code{
	connect.CodeCanceled:           Canceled,
	connect.CodeUnknown:            Unknown,
	connect.CodeInvalidArgument:    InvalidArgument,
	connect.CodeDeadlineExceeded:   DeadlineExceeded,
	connect.CodeNotFound:           NotFound,
	connect.CodeAlreadyExists:      AlreadyExists,
	connect.CodePermissionDenied:   PermissionDenied,
	connect.CodeResourceExhausted:  ResourceExhausted,
	connect.CodeFailedPrecondition: FailedPrecondition,
	connect.CodeAborted:            Aborted,
	connect.CodeOutOfRange:         OutOfRange,
	connect.CodeUnimplemented:      Unimplemented,
	connect.CodeInternal:           Internal,
	connect.CodeUnavailable:        Unavailable,
	connect.CodeDataLoss:           DataLoss,
	connect.CodeUnauthenticated:    Unauthenticated,
}

func NewCodeFromConnectError(err error) Code {
	cc := connect.CodeOf(err)
	c, ok := connectCodeToCodeMap[cc]
	if !ok {
		return Unknown
	}
	return c
}

func (c Code) ConnectCode() connect.Code {
	if c == OK {
		return 0
	}
	code, ok := codeToConnectCodeMap[c]
	if !ok {
		return connect.CodeUnknown
	}
	return code
}

func (c Code) HTTPCode() int {
	switch c {
	case OK:
		return http.StatusOK
	case Canceled:
		return 499
	case Unknown:
		return http.StatusInternalServerError
	case InvalidArgument:
		return http.StatusBadRequest
	case DeadlineExceeded:
		return http.StatusGatewayTimeout
	case NotFound:
		return http.StatusNotFound
	case AlreadyExists:
		return http.StatusConflict
	case PermissionDenied:
		return http.StatusForbidden
	case ResourceExhausted:
		return http.StatusTooManyRequests
	case FailedPrecondition:
		return http.StatusPreconditionFailed
	case Aborted:
		return http.StatusConflict
	case OutOfRange:
		return http.StatusBadRequest
	case Unimplemented:
		return http.StatusNotImplemented
	case Internal:
		return http.StatusInternalServerError
	case Unavailable:
		return http.StatusServiceUnavailable
	case DataLoss:
		return http.StatusInternalServerError
	case Unauthenticated:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}
