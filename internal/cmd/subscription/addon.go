package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/spf13/cobra"
)

// addonSvc is what the mutating add-on commands need: mutate, then wait on the
// webhook-synced status.
type addonAttachSvc interface {
	AddOnAttacher
	StatusGetter
}

type addonDetachSvc interface {
	AddOnDetacher
	StatusGetter
}

func addonCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "addon",
		Short: "Manage subscription add-ons",
		Long: "List, buy, or remove subscription add-ons. Add-ons grant extra features on top of the " +
			"paid plan and are billed on the plan's cadence. Available add-ons: " + strings.Join(AddOnTokens(), ", ") + ".",
	}
	parent.AddCommand(addonListCmd(a))
	parent.AddCommand(addonAddCmd(a))
	parent.AddCommand(addonRemoveCmd(a))
	return parent
}

func addonListCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List purchased add-ons",
		Long: "List the add-ons purchased on the subscription. Add-ons granted through entitlements " +
			"without a purchase (e.g. a comped plan) show under `safedep subscription status --entitlements`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			acct, err := NewService(client.Connection()).Status(cmd.Context())
			if err != nil {
				return err
			}
			return a.Output.Print(&addonListResult{addOns: acct.ActiveAddOns})
		},
	}
}

func addonAddCmd(a *app.App) *cobra.Command {
	var (
		acceptTerms bool
		wait        bool
		timeout     time.Duration
	)
	cmd := &cobra.Command{
		Use:   "add <add-on>",
		Short: "Buy an add-on",
		Long: "Buy an add-on on the active paid subscription. The add-on is billed on the plan's cadence " +
			"and charged immediately against the payment method on file. Requires acceptance of the add-on terms.",
		Args: addOnArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			addOn := args[0]
			if !acceptTerms {
				tui.Warning("Buying the %q add-on charges your payment method on file immediately.", addOn)
				tui.Info("Terms: %s", termsURL)
				return errors.New("re-run with --accept-terms to confirm")
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runAddonAdd(cmd.Context(), NewService(client.Connection()), addOn, wait, timeout)
			if err != nil {
				return err
			}
			tui.Success("Add-on %q purchased (terms %s accepted).", addOn, termsVersion)
			return a.Output.Print(res)
		},
	}
	cmd.Flags().BoolVar(&acceptTerms, "accept-terms", false, "accept the add-on billing terms ("+termsURL+")")
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for the add-on to activate on the subscription")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for activation")
	return cmd
}

func addonRemoveCmd(a *app.App) *cobra.Command {
	var (
		wait    bool
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "remove <add-on>",
		Short: "Remove an add-on",
		Long:  "Remove an add-on from the subscription. The provider credits the unused portion on the next invoice.",
		Args:  addOnArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runAddonRemove(cmd.Context(), NewService(client.Connection()), args[0], wait, timeout)
			if err != nil {
				return err
			}
			tui.Success("Add-on %q removed.", args[0])
			return a.Output.Print(res)
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for the add-on to clear from the subscription")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait")
	return cmd
}

// addOnArg validates a single add-on token argument, listing valid tokens on a
// miss so the error is actionable before any network call.
func addOnArg(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	_, err := parseAddOn(args[0])
	return err
}

func runAddonAdd(ctx context.Context, svc addonAttachSvc, addOn string, wait bool, timeout time.Duration) (*addonMutationResult, error) {
	addOns, err := svc.AttachAddOn(ctx, addOn, termsVersion)
	if err != nil {
		return nil, err
	}
	if !wait {
		return &addonMutationResult{addOns: addOns}, nil
	}
	// The attach charges synchronously, but the entitlement lands via the
	// webhook that syncs subscription items. Wait until status reflects it.
	acct, err := pollUntil(ctx, svc, addOnPresenceWaiter(addOn, true), timeout)
	if err != nil {
		return nil, err
	}
	return &addonMutationResult{addOns: acct.ActiveAddOns}, nil
}

func runAddonRemove(ctx context.Context, svc addonDetachSvc, addOn string, wait bool, timeout time.Duration) (*addonMutationResult, error) {
	addOns, err := svc.DetachAddOn(ctx, addOn)
	if err != nil {
		return nil, err
	}
	if !wait {
		return &addonMutationResult{addOns: addOns}, nil
	}
	acct, err := pollUntil(ctx, svc, addOnPresenceWaiter(addOn, false), timeout)
	if err != nil {
		return nil, err
	}
	return &addonMutationResult{addOns: acct.ActiveAddOns}, nil
}

type addonListResult struct{ addOns []string }

func (r *addonListResult) RenderJSON() ([]byte, error) {
	// Always a list, never null, so consumers can iterate unconditionally.
	return json.MarshalIndent(map[string]any{"add_ons": append([]string{}, r.addOns...)}, "", "  ")
}

func (r *addonListResult) RenderPlain() string {
	if len(r.addOns) == 0 {
		return "add_ons\t-"
	}
	lines := make([]string, 0, len(r.addOns))
	for _, a := range r.addOns {
		lines = append(lines, "add_on\t"+a)
	}
	return strings.Join(lines, "\n")
}

func (r *addonListResult) RenderTable() string {
	if len(r.addOns) == 0 {
		return section.Join(
			panel.New("Add-ons").Field("Purchased", "-").Render(),
			section.Hint("Buy one: safedep subscription addon add <add-on>"),
		)
	}
	p := panel.New("Add-ons")
	for _, a := range r.addOns {
		p = p.Field("Purchased", a)
	}
	return p.Render()
}

type addonMutationResult struct{ addOns []string }

func (r *addonMutationResult) RenderJSON() ([]byte, error) {
	return (&addonListResult{addOns: r.addOns}).RenderJSON()
}

func (r *addonMutationResult) RenderPlain() string {
	return (&addonListResult{addOns: r.addOns}).RenderPlain()
}

func (r *addonMutationResult) RenderTable() string {
	return (&addonListResult{addOns: r.addOns}).RenderTable()
}
