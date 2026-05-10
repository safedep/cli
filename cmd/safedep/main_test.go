package main

import (
	"errors"
	"testing"

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

	t.Run("keeps non unauthenticated grpc errors unchanged", func(t *testing.T) {
		err := status.Error(codes.PermissionDenied, "forbidden")

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
	})

	t.Run("keeps non grpc errors unchanged", func(t *testing.T) {
		err := errors.New("plain error")

		got := normalizeRunError(err)

		assert.Equal(t, err, got)
	})
}
