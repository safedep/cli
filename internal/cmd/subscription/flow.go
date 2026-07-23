package subscription

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cli/browser"
	"github.com/safedep/dry/log"
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
	if !interactive() {
		return
	}
	if err := browser.OpenURL(url); err != nil {
		log.Warnf("subscription: could not open browser automatically: %v", err)
		tui.Info("Open the URL above manually to continue.")
	}
}

// pollUntilStatus polls the account status until it reaches one of want, or
// the timeout elapses. The timeout is enforced as a hard deadline on the whole
// operation, including each status RPC (so a stalled control-plane call cannot
// outlast --timeout), and a timeout is returned as an error: a waited operation
// that does not confirm is a failure, not a silent success.
func pollUntilStatus(ctx context.Context, svc StatusGetter, want map[string]bool, timeout time.Duration) (*AccountStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	backoff := pollInitial
	for {
		acct, err := svc.Status(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, timeoutError(timeout)
			}
			return nil, err
		}
		if want[acct.Status] {
			return acct, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, timeoutError(timeout)
			}
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff = time.Duration(float64(backoff) * pollFactor); backoff > pollMax {
			backoff = pollMax
		}
	}
}

func timeoutError(d time.Duration) error {
	return fmt.Errorf("timed out after %s waiting for the account to update: re-check with `safedep subscription status`", d)
}
