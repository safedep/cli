package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshAndPersistIfExpiredRequiresLoginWithoutRefreshToken(t *testing.T) {
	creds := expiredCredentials(t, "")

	_, err := RefreshAndPersistIfExpired(context.Background(), nil, creds, nil)

	requireLoginRequiredError(t, err)
	assert.Contains(t, err.Error(), "session expired")
}

func TestRefreshAndPersistIfExpiredRequiresLoginWhenRefreshFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":"invalid_grant"}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	t.Setenv(envTokenURL, server.URL)
	creds := expiredCredentials(t, "invalid-refresh-token")

	_, err := RefreshAndPersistIfExpired(context.Background(), nil, creds, nil)

	requireLoginRequiredError(t, err)
	assert.ErrorIs(t, err, ErrRefreshFailed)
}

func expiredCredentials(t *testing.T, refreshToken string) *cloud.Credentials {
	t.Helper()
	token := createTestToken(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})
	creds, err := cloud.NewTokenCredential(token, refreshToken, "example")
	require.NoError(t, err)
	return creds
}

func requireLoginRequiredError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	usefulErr, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, usefulerror.ErrAuthenticationFailed, usefulErr.Code())
	assert.Equal(t, "Authentication required", usefulErr.HumanError())
	assert.Equal(t, "Run `safedep auth login` and retry.", usefulErr.Help())
}
