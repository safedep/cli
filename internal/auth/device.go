package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cli/oauth/api"
	"github.com/cli/oauth/device"
)

// ErrEmailNotVerified is returned by RunDeviceFlow when Auth0 rejects the
// device authorisation because the user's email address is unverified.
// Callers can detect it with errors.Is to offer a retry path.
var ErrEmailNotVerified = errors.New("auth: email not verified")

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

// DeviceFlowRetry defines when and how to retry a failed device flow attempt.
// Each policy is applied at most once: if ShouldRetry matches, OnRetry fires,
// and the flow is attempted again. If the retry also fails with a matching
// error, OnExhausted returns the terminal error shown to the user.
type DeviceFlowRetry struct {
	ShouldRetry func(err error) bool
	OnRetry     func(err error)
	OnExhausted func() error
}

// RunDeviceFlow performs a complete OAuth2 device-code authorisation against
// the configured SafeDep identity provider. Optional retry policies are
// applied in order: the first matching policy fires its OnRetry side-effect
// and retries the flow once. Existing callers that pass no retries are
// unaffected.
func RunDeviceFlow(ctx context.Context, sink DeviceFlowSink, retries ...DeviceFlowRetry) (*DeviceFlowResult, error) {
	result, err := runDeviceFlowOnce(ctx, sink)
	if err == nil {
		return result, nil
	}

	for _, r := range retries {
		if !r.ShouldRetry(err) {
			continue
		}
		if r.OnRetry != nil {
			r.OnRetry(err)
		}
		result, err = runDeviceFlowOnce(ctx, sink)
		if err != nil {
			if r.ShouldRetry(err) && r.OnExhausted != nil {
				return nil, r.OnExhausted()
			}
			return nil, err
		}
		return result, nil
	}

	return nil, err
}

func runDeviceFlowOnce(ctx context.Context, sink DeviceFlowSink) (*DeviceFlowResult, error) {
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
		if isEmailVerificationError(err) {
			return nil, fmt.Errorf("%w: check your inbox for a verification email from SafeDep and click the link", ErrEmailNotVerified)
		}
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

// isEmailVerificationError detects the Auth0 access_denied error that occurs
// when the user has not yet verified their email address.
func isEmailVerificationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access_denied") && strings.Contains(msg, "verify your email")
}
