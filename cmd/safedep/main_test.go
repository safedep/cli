package main

import (
	"errors"
	"fmt"
	"testing"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNormalizeRunError(t *testing.T) {
	t.Run("maps grpc unauthenticated to common login hint", func(t *testing.T) {
		err := status.Error(codes.Unauthenticated, "unexpected HTTP status code received from server: 401 (Unauthorized)")

		got := normalizeRunError(err)

		assert.EqualError(t, got, "not authenticated: run `safedep auth login`")
	})

	t.Run("renders a typed grpc reason without internal details", func(t *testing.T) {
		err := fmt.Errorf(
			"project scan create: %w",
			usefulerror.NewStatus(codes.FailedPrecondition, "service execution failed").
				WithReason(errorv1.ErrorReason_ERROR_REASON_PROJECT_NOT_SCANNABLE).
				Err(),
		)

		got := normalizeRunError(err)

		assert.EqualError(
			t,
			got,
			"Project not scannable\nCode: bad_request\n"+
				"Help: The project has no supported, usable source. Refresh the project source and retry.",
		)
		assert.NotContains(t, got.Error(), "project scan create")
		assert.NotContains(t, got.Error(), "service execution failed")
		assert.NotContains(t, got.Error(), "rpc error")
	})

	t.Run("renders a generic grpc error without internal details", func(t *testing.T) {
		err := status.Error(codes.ResourceExhausted, "nested internal quota response")

		got := normalizeRunError(err)

		assert.EqualError(
			t,
			got,
			"Quota exceeded\nCode: quota_exceeded\n"+
				"Help: Reduce request frequency or increase your quota.",
		)
		assert.NotContains(t, got.Error(), "nested internal quota response")
		assert.NotContains(t, got.Error(), "rpc error")
	})

	t.Run("renders a package scan entitlement error without internal details", func(t *testing.T) {
		err := fmt.Errorf(
			"package scan: submit: %w",
			usefulerror.NewStatus(codes.ResourceExhausted, "service execution failed").
				WithReason(errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE).
				Err(),
		)

		got := normalizeRunError(err)

		assert.EqualError(
			t,
			got,
			"Feature unavailable\nCode: missing_entitlements\n"+
				"Help: Access to this feature requires a SafeDep subscription. See https://safedep.io/pricing",
		)
		assert.NotContains(t, got.Error(), "package scan")
		assert.NotContains(t, got.Error(), "service execution failed")
		assert.NotContains(t, got.Error(), "rpc error")
	})

	t.Run("includes a useful error reference URL", func(t *testing.T) {
		err := usefulerror.NewUsefulError().
			WithHumanError("Project not scannable").
			WithCode("bad_request").
			WithHelp("Refresh the project source and retry.").
			WithReferenceURL("https://docs.safedep.io/errors/project-not-scannable")

		got := normalizeRunError(err)

		assert.EqualError(
			t,
			got,
			"Project not scannable\nCode: bad_request\n"+
				"Help: Refresh the project source and retry.\n"+
				"More: https://docs.safedep.io/errors/project-not-scannable",
		)
	})

	t.Run("keeps non grpc errors unchanged", func(t *testing.T) {
		err := errors.New("plain error")

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
	})
}
