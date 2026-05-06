package jfrog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/safedep/dry/log"
)

// jfrogSystemVersion is the response shape of GET /xray/api/v1/system/version.
// Only the fields we surface to the operator are decoded; XRay returns more.
type jfrogSystemVersion struct {
	XrayVersion  string `json:"xray_version"`
	XrayRevision string `json:"xray_revision"`
}

// validateJFrog performs a lightweight pre-flight check against XRay's
// system/version endpoint to fail fast at startup if the URL or token are
// wrong. Returns the running XRay version on success.
//
// This lives outside pusher.go because it is a one-shot connectivity probe,
// not a record-pushing concern. Keeping it separate means the daemon's
// data-plane code (pusher) does not grow validation responsibilities.
//
// Status code mapping (chosen for actionable error messages):
//   - 200          : URL + token + permissions all OK
//   - 401          : token invalid or expired
//   - 403          : token valid but lacks "Manage Xray Metadata" permission
//   - 404          : URL points somewhere that is not an XRay instance
//   - other / net  : surfaced verbatim with the response body for diagnosis
func validateJFrog(ctx context.Context, cfg JFrogConfig) (string, error) {
	url := strings.TrimRight(cfg.URL, "/") + "/xray/api/v1/system/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("jfrog validate: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jfrog validate: cannot reach %s: %w", cfg.URL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Warnf("jfrog validate: close response body: %v", err)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		log.Warnf("jfrog validate: read response body: %v", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var v jfrogSystemVersion
		if err := json.Unmarshal(body, &v); err != nil {
			return "", fmt.Errorf("jfrog validate: parse version: %w", err)
		}
		return v.XrayVersion, nil
	case http.StatusUnauthorized:
		return "", fmt.Errorf("jfrog validate: 401 Unauthorized - access token is invalid or expired")
	case http.StatusForbidden:
		return "", fmt.Errorf("jfrog validate: 403 Forbidden - token lacks 'Manage Xray Metadata' permission")
	case http.StatusNotFound:
		return "", fmt.Errorf("jfrog validate: 404 Not Found - %s does not appear to be an XRay endpoint", cfg.URL)
	default:
		return "", fmt.Errorf("jfrog validate: unexpected status %d: %s", resp.StatusCode, string(body))
	}
}
