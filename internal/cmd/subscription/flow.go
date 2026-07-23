package subscription

import (
	"context"
	"time"

	"github.com/cli/browser"
	"github.com/safedep/dry/tui"
)

// Web app billing pages reused as checkout/portal return targets, matching
// app.safedep.io. Completion is webhook-gated, so these are only "return to
// your terminal" landing pages; the CLI polls status for the real result.
const (
	appBaseURL         = "https://app.safedep.io"
	checkoutSuccessURL = appBaseURL + "/settings/billing/success"
	checkoutCancelURL  = appBaseURL + "/settings/billing"
	portalReturnURL    = appBaseURL + "/settings/billing"
	termsURL           = "https://safedep.io/terms/"
)

const (
	pollInitial = 2 * time.Second
	pollFactor  = 1.5
	pollMax     = 10 * time.Second
)

// openInBrowser opens url for humans (best-effort) and always prints it so a
// headless or agent session can follow it manually.
func openInBrowser(url, prompt string) {
	tui.Info("%s\n  %s", prompt, url)
	if interactive() {
		_ = browser.OpenURL(url)
	}
}

// pollUntilStatus polls the account status until it reaches one of want or
// the timeout elapses. Returns the last observed status on timeout.
func pollUntilStatus(ctx context.Context, svc StatusGetter, want map[string]bool, timeout time.Duration) (*AccountStatus, bool, error) {
	deadline := time.Now().Add(timeout)
	backoff := pollInitial
	for {
		acct, err := svc.Status(ctx)
		if err != nil {
			return nil, false, err
		}
		if want[acct.Status] {
			return acct, true, nil
		}
		if time.Now().Add(backoff).After(deadline) {
			return acct, false, nil
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff = time.Duration(float64(backoff) * pollFactor); backoff > pollMax {
			backoff = pollMax
		}
	}
}
