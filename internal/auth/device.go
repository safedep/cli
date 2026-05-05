package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cli/oauth/api"
	"github.com/cli/oauth/device"
)

// DeviceFlowResult is the outcome of a successful OAuth2 device-code
// authorisation: the access + refresh token pair returned by the IdP.
type DeviceFlowResult struct {
	AccessToken  string
	RefreshToken string
}

// DeviceFlowSink reports the verification URL and user code to the user.
// Implementations decide how to present them (TUI banner, opening a
// browser, etc.). The sink runs once before polling begins.
type DeviceFlowSink func(verificationURL, userCode string)

// RunDeviceFlow performs a complete OAuth2 device-code authorisation
// against the configured SafeDep identity provider. It blocks until the
// user completes the flow in their browser or the IdP returns an error.
func RunDeviceFlow(ctx context.Context, sink DeviceFlowSink) (*DeviceFlowResult, error) {
	httpClient := http.DefaultClient

	code, err := device.RequestCode(httpClient,
		DeviceCodeURL(),
		ClientID(),
		CLIScopes,
		device.WithAudience(Audience()),
	)
	if err != nil {
		return nil, fmt.Errorf("auth: request device code: %w", err)
	}

	if sink != nil {
		sink(code.VerificationURIComplete, code.UserCode)
	}

	token, err := device.Wait(ctx, httpClient, TokenURL(), device.WaitOptions{
		ClientID:   ClientID(),
		DeviceCode: code,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: device flow: %w", err)
	}

	return &DeviceFlowResult{
		AccessToken:  token.Token,
		RefreshToken: refreshFromAccessToken(token),
	}, nil
}

func refreshFromAccessToken(t *api.AccessToken) string {
	if t == nil {
		return ""
	}
	return t.RefreshToken
}
