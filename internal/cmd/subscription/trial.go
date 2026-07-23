package subscription

import (
	"context"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

func trialCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "trial",
		Short: "Manage the free trial",
		Long:  "Activate the free trial subscription for the tenant account.",
	}
	parent.AddCommand(trialEnableCmd(a))
	return parent
}

// trialSvc is what trial enable needs: ensure a customer, activate, poll.
type trialSvc interface {
	customerSvc
	TrialActivator
	StatusGetter
}

func trialEnableCmd(a *app.App) *cobra.Command {
	var (
		form    customerForm
		wait    bool
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Activate the free trial",
		Long:  "Activate the free trial subscription. Creates a billing profile first if none exists (interactive on a terminal, flags otherwise).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			svc := NewService(client.Connection())
			acct, confirmed, err := runTrialEnable(cmd.Context(), svc, form, wait, timeout)
			if err != nil {
				return err
			}
			if confirmed {
				tui.Success("Trial activated.")
			} else {
				tui.Warning("Trial requested. Activation is still syncing - re-check with `safedep subscription status`.")
			}
			return a.Output.Print(&statusResult{acct: acct})
		},
	}
	addCustomerFlags(cmd, &form)
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for the trial to become active")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "maximum time to wait for activation")
	return cmd
}

// runTrialEnable activates the trial and reports whether the account was
// observed in a trial/active state. confirmed is false when activation is
// still syncing (--wait=false, or a wait that timed out), so the caller does
// not claim success prematurely.
func runTrialEnable(ctx context.Context, svc trialSvc, form customerForm, wait bool, timeout time.Duration) (*AccountStatus, bool, error) {
	if err := ensureCustomer(ctx, svc, form); err != nil {
		return nil, false, err
	}
	if err := svc.ActivateTrial(ctx); err != nil {
		return nil, false, err
	}
	want := map[string]bool{statusActiveTrial: true, statusActive: true}
	if !wait {
		acct, err := svc.Status(ctx)
		if err != nil {
			return nil, false, err
		}
		return acct, want[acct.Status], nil
	}
	acct, ok, err := pollUntilStatus(ctx, svc, want, timeout)
	if err != nil {
		return nil, false, err
	}
	return acct, ok, nil
}
