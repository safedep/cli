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
			acct, err := runTrialEnable(cmd.Context(), svc, form, wait, timeout)
			if err != nil {
				return err
			}
			tui.Success("Trial activated.")
			return a.Output.Print(&statusResult{acct: acct})
		},
	}
	addCustomerFlags(cmd, &form)
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for the trial to become active")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "maximum time to wait for activation")
	return cmd
}

func runTrialEnable(ctx context.Context, svc trialSvc, form customerForm, wait bool, timeout time.Duration) (*AccountStatus, error) {
	if err := ensureCustomer(ctx, svc, form); err != nil {
		return nil, err
	}
	if err := svc.ActivateTrial(ctx); err != nil {
		return nil, err
	}
	if !wait {
		return svc.Status(ctx)
	}
	acct, _, err := pollUntilStatus(ctx, svc, map[string]bool{statusActiveTrial: true, statusActive: true}, timeout)
	if err != nil {
		return nil, err
	}
	return acct, nil
}
