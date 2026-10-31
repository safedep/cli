package rpcerror

import (
	"errors"
	"testing"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReason(t *testing.T) {
	detail := &errorv1.ErrorDetail{}
	detail.SetReason(errorv1.ErrorReason_ERROR_REASON_POLICY_CHANGE_STALE)
	st, err := status.New(codes.Aborted, "stale").WithDetails(detail)
	require.NoError(t, err)

	tests := []struct {
		name string
		err  error
		want errorv1.ErrorReason
	}{
		{"detail", st.Err(), errorv1.ErrorReason_ERROR_REASON_POLICY_CHANGE_STALE},
		{"status without detail", status.Error(codes.Aborted, "x"), errorv1.ErrorReason_ERROR_REASON_UNSPECIFIED},
		{"not a status", errors.New("x"), errorv1.ErrorReason_ERROR_REASON_UNSPECIFIED},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Reason(tt.err), tt.name)
	}
}
