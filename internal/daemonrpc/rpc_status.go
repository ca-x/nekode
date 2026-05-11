package daemonrpc

import (
	"fmt"

	"connectrpc.com/connect"
)

type rpcCode = connect.Code

// These shims keep the daemon business logic transport-neutral while preserving
// the existing status.Error(codes.X, ...) call sites.
var codes = struct {
	Canceled           rpcCode
	Unknown            rpcCode
	InvalidArgument    rpcCode
	DeadlineExceeded   rpcCode
	NotFound           rpcCode
	AlreadyExists      rpcCode
	PermissionDenied   rpcCode
	ResourceExhausted  rpcCode
	FailedPrecondition rpcCode
	Aborted            rpcCode
	OutOfRange         rpcCode
	Unimplemented      rpcCode
	Internal           rpcCode
	Unavailable        rpcCode
	DataLoss           rpcCode
	Unauthenticated    rpcCode
}{
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

var status = struct {
	Error  func(rpcCode, string) error
	Errorf func(rpcCode, string, ...any) error
}{
	Error: func(code rpcCode, message string) error {
		return connect.NewError(code, fmt.Errorf("%s", message))
	},
	Errorf: func(code rpcCode, format string, args ...any) error {
		return connect.NewError(code, fmt.Errorf(format, args...))
	},
}
