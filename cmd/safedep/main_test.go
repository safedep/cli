package main

import (
	"errors"
	"fmt"
	"testing"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNormalizeRunError(t *testing.T) {
	t.Run("maps grpc unauthenticated to common login hint", func(t *testing.T) {
		err := status.Error(codes.Unauthenticated, "unexpected HTTP status code received from server: 401 (Unauthorized)")

		got := normalizeRunError(err)

		assert.EqualError(t, got, "not authenticated: run `safedep auth login`")
	})

	t.Run("preserves a typed grpc reason for the dry renderer", func(t *testing.T) {
		err := fmt.Errorf(
			"project scan create: %w",
			usefulerror.NewStatus(codes.FailedPrecondition, "service execution failed").
				WithReason(errorv1.ErrorReason_ERROR_REASON_PROJECT_NOT_SCANNABLE).
				Err(),
		)

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
		assert.Equal(t, codes.FailedPrecondition, status.Code(got))
		reason, ok := usefulerror.ReasonOf(got)
		require.True(t, ok)
		assert.Equal(t, errorv1.ErrorReason_ERROR_REASON_PROJECT_NOT_SCANNABLE, reason)
	})

	t.Run("preserves a generic grpc error for the dry renderer", func(t *testing.T) {
		err := status.Error(codes.ResourceExhausted, "nested internal quota response")

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
		assert.Equal(t, codes.ResourceExhausted, status.Code(got))
	})

	t.Run("preserves a package scan entitlement error for the dry renderer", func(t *testing.T) {
		rpcStatus, err := status.New(codes.ResourceExhausted, "service execution failed").WithDetails(
			&errdetails.ErrorInfo{
				Reason: usefulerror.ErrAppQuotaExceeded,
				Domain: usefulerror.DefaultErrorDomain,
				Metadata: map[string]string{
					"reason": usefulerror.ErrAppQuotaReasonFeatureNotAvailable,
				},
			},
		)
		require.NoError(t, err)

		err = fmt.Errorf(
			"package scan: submit: %w",
			rpcStatus.Err(),
		)

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
		assert.Equal(t, codes.ResourceExhausted, status.Code(got))
	})

	t.Run("preserves a useful error reference URL for the dry renderer", func(t *testing.T) {
		err := usefulerror.NewUsefulError().
			WithHumanError("Project not scannable").
			WithCode("bad_request").
			WithHelp("Refresh the project source and retry.").
			WithReferenceURL("https://docs.safedep.io/errors/project-not-scannable")

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
	})

	t.Run("keeps non grpc errors unchanged", func(t *testing.T) {
		err := errors.New("plain error")

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
	})
}
