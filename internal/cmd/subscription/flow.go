package subscription

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cli/browser"
	"github.com/safedep/dry/log"
	"github.com/safedep/dry/tui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// statusWaiter classifies observed account statuses during a wait. A status
// in Done ends the wait successfully; a status in Fail ends it with that
// status's error (e.g. a terminal payment failure with remediation); anything
// else keeps polling. Kept as data so new desired/terminal states are a
// one-line addition, not new control flow.
type statusWaiter struct {
	Done map[string]bool
	Fail map[string]error
}

// pollUntilStatus polls the account status until the waiter resolves it, or the
// timeout elapses. The timeout is a hard deadline on the whole operation,
// including each status RPC (so a stalled control-plane call cannot outlast
// --timeout). A timeout returns an error: a waited operation that never
// confirms is a failure, not a silent success. Transient read failures
// (network blips) are retried within the deadline rather than failing the
// command, since the underlying mutation may already have succeeded.
func pollUntilStatus(ctx context.Context, svc StatusGetter, w statusWaiter, timeout time.Duration) (*AccountStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	backoff := pollInitial
	for {
		acct, err := svc.Status(ctx)
		switch {
		case err == nil:
			if w.Done[acct.Status] {
				return acct, nil
			}
			if ferr, ok := w.Fail[acct.Status]; ok {
				return nil, ferr
			}
			// Not a resolved state yet: keep waiting.
		case isDeadlineExceeded(ctx, err):
			return nil, timeoutError(timeout)
		case isTransient(err):
			// A temporary read failure: the mutation may already have
			// succeeded, so retry within the overall deadline.
			log.Warnf("subscription: transient error reading status, retrying: %v", err)
		default:
			return nil, err
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

// isDeadlineExceeded reports whether err is our overall-timeout expiring,
// covering both the context sentinel and a gRPC status carrying
// codes.DeadlineExceeded (the two do not satisfy errors.Is each other).
func isDeadlineExceeded(ctx context.Context, err error) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		errors.Is(err, context.DeadlineExceeded) ||
		status.Code(err) == codes.DeadlineExceeded
}

// isTransient reports whether a gRPC error is worth retrying within the wait.
func isTransient(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.Aborted:
		return true
	default:
		return false
	}
}

func timeoutError(d time.Duration) error {
	return fmt.Errorf("timed out after %s waiting for the account to update: re-check with `safedep subscription status`", d)
}
