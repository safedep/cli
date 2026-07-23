package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

const defaultSeats = 5

// createSvc is what subscribe needs: ensure a customer, checkout, poll.
type createSvc interface {
	customerSvc
	Subscriber
	StatusGetter
}

func createCmd(a *app.App) *cobra.Command {
	var (
		form    customerForm
		seats   uint32
		wait    bool
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Subscribe to the Professional plan",
		Long: "Subscribe the tenant account to the Professional plan via checkout. Creates a billing " +
			"profile first if none exists. Enterprise plans are custom - contact sales.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if seats < 1 {
				return errors.New("--seats must be at least 1")
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			svc := NewService(client.Connection())
			res, err := runCreate(cmd.Context(), svc, form, seats, wait, timeout)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	addCustomerFlags(cmd, &form)
	cmd.Flags().Uint32Var(&seats, "seats", defaultSeats, "number of seats (min 1)")
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for the subscription to become active after checkout")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "maximum time to wait for activation")
	return cmd
}

func runCreate(ctx context.Context, svc createSvc, form customerForm, seats uint32, wait bool, timeout time.Duration) (*createResult, error) {
	if err := ensureCustomer(ctx, svc, form); err != nil {
		return nil, err
	}

	checkout, err := svc.Checkout(ctx, CheckoutInput{
		Seats: seats, SuccessURL: checkoutSuccessURL, CancelURL: checkoutCancelURL,
	})
	if err != nil {
		return nil, err
	}

	switch checkout.Outcome {
	case checkoutSuccess:
		acct, err := svc.Status(ctx)
		if err != nil {
			return nil, err
		}
		return &createResult{acct: acct, alreadyActive: true}, nil
	case checkoutError:
		return nil, fmt.Errorf("checkout failed: %s (%s)", checkout.ErrorMessage, checkout.ErrorCode)
	}

	// NEED_CHECKOUT_COMPLETION: hand off to the browser.
	openInBrowser(checkout.URL, fmt.Sprintf("Opening checkout for Professional, %d seats...", seats))
	if !wait {
		return &createResult{checkoutURL: checkout.URL}, nil
	}

	tui.Info("Waiting for checkout to complete...")
	acct, err := pollUntilStatus(ctx, svc, map[string]bool{statusActive: true}, timeout)
	if err != nil {
		return nil, err
	}
	return &createResult{acct: acct}, nil
}

type createResult struct {
	acct          *AccountStatus
	alreadyActive bool
	checkoutURL   string // set only with --wait=false
}

func (r *createResult) RenderJSON() ([]byte, error) {
	if r.acct != nil {
		return (&statusResult{acct: r.acct}).RenderJSON()
	}
	return json.MarshalIndent(map[string]any{"status": "need_checkout_completion", "checkout_url": r.checkoutURL}, "", "  ")
}

func (r *createResult) RenderPlain() string {
	if r.acct != nil {
		return (&statusResult{acct: r.acct}).RenderPlain()
	}
	return "checkout_url\t" + r.checkoutURL
}

func (r *createResult) RenderTable() string {
	if r.acct != nil {
		return (&statusResult{acct: r.acct}).RenderTable()
	}
	return "Complete checkout in your browser, then check: safedep subscription status\n  " + r.checkoutURL
}
