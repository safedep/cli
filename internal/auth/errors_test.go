package auth

import (
	"errors"
	"testing"

	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginRequiredError(t *testing.T) {
	cause := errors.New("credentials not found")

	err := LoginRequiredError(cause)

	usefulErr, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, usefulerror.ErrAuthenticationFailed, usefulErr.Code())
	assert.Equal(t, "Authentication required", usefulErr.HumanError())
	assert.Equal(t, "Run `safedep auth login` and retry.", usefulErr.Help())
	assert.ErrorIs(t, err, cause)
}
