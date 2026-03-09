package clog

import (
	"connectrpc.com/connect"
)

type Level int

const (
	LevelDebug Level = iota + 1
	LevelInfo
	LevelWarn
	LevelError
)

func HTTPStatusToLevel(status int) Level {
	switch {
	case status >= 100 && status < 400:
		return LevelInfo
	case status == 499:
		return LevelInfo
	case status >= 400 && status < 500:
		return LevelWarn
	case status >= 500:
		return LevelError
	default:
		return LevelError
	}
}

func ConnectCodeToLevel(code connect.Code) Level {
	switch code {
	case connect.CodeCanceled:
		return LevelInfo
	case connect.CodeUnknown:
		return LevelError
	case connect.CodeInvalidArgument:
		return LevelInfo
	case connect.CodeDeadlineExceeded:
		return LevelInfo
	case connect.CodeNotFound:
		return LevelInfo
	case connect.CodeAlreadyExists:
		return LevelInfo
	case connect.CodePermissionDenied:
		return LevelInfo
	case connect.CodeResourceExhausted:
		return LevelError
	case connect.CodeFailedPrecondition:
		return LevelInfo
	case connect.CodeAborted:
		return LevelInfo
	case connect.CodeOutOfRange:
		return LevelInfo
	case connect.CodeUnimplemented:
		return LevelError
	case connect.CodeInternal:
		return LevelError
	case connect.CodeUnavailable:
		return LevelError
	case connect.CodeDataLoss:
		return LevelError
	case connect.CodeUnauthenticated:
		return LevelInfo
	}
	return LevelError
}
