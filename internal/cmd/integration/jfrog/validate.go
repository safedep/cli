package jfrog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/safedep/dry/log"
)

// validateJFrog performs a pre-flight check that proves three things in a
// single round trip: the URL is reachable, the access token is valid, and
// the token has XRay read permissions.
//
// We probe GET /xray/api/v1/policies (authenticated, read-only) instead of
// /system/version, because system/version returns 200 even without an auth
// header — using it would silently pass with a wrong token.
//
// This lives outside pusher.go because it is a one-shot connectivity probe,
// not a record-pushing concern. Keeping it separate means the daemon's
// data-plane code (pusher) does not grow validation responsibilities.
//
// Status code mapping (chosen for actionable error messages):
//   - 200          : URL + token + permissions all OK
//   - 401          : token invalid or expired
//   - 403          : token valid but lacks XRay read permissions
//   - 404          : URL points somewhere that is not an XRay instance
//   - other / net  : surfaced verbatim with the response body for diagnosis
func validateJFrog(ctx context.Context, cfg jfrogConfig) error {
	url := strings.TrimRight(cfg.url, "/") + "/xray/api/v1/policies"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("jfrog validate: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.accessToken)
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("jfrog validate: cannot reach %s: %w", cfg.url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Internal diagnostic: deferred cleanup failure is not actionable
			// by the operator. dry/log per AGENTS.md convention.
			log.Warnf("jfrog validate: close response body: %v", err)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		// Internal diagnostic: read failure on the diagnostic body itself.
		log.Warnf("jfrog validate: read response body: %v", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("jfrog validate: 401 Unauthorized - access token is invalid or expired")
	case http.StatusForbidden:
		return fmt.Errorf("jfrog validate: 403 Forbidden - token lacks XRay read permission")
	case http.StatusNotFound:
		return fmt.Errorf("jfrog validate: 404 Not Found - %s does not appear to be an XRay endpoint", cfg.url)
	default:
		return fmt.Errorf("jfrog validate: unexpected status %d: %s", resp.StatusCode, string(body))
	}
}
