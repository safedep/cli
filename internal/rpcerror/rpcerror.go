// Package rpcerror reads the SafeDep business reason of a gRPC error. Every
// command package that maps a reason to a recovery reads it here.
package rpcerror

import (
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"google.golang.org/grpc/status"
)

// Reason returns the ErrorReason of the ErrorDetail in a gRPC status error.
// It returns ERROR_REASON_UNSPECIFIED when the error has no such detail, so a
// caller falls back to the status code.
func Reason(err error) errorv1.ErrorReason {
	st, ok := status.FromError(err)
	if !ok {
		return errorv1.ErrorReason_ERROR_REASON_UNSPECIFIED
	}
	for _, d := range st.Details() {
		if detail, ok := d.(*errorv1.ErrorDetail); ok {
			return detail.GetReason()
		}
	}
	return errorv1.ErrorReason_ERROR_REASON_UNSPECIFIED
}
