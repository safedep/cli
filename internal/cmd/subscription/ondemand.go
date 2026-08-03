package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/spf13/cobra"
)

func ondemandCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "ondemand",
		Short: "Manage on-demand (overage) billing",
		Long:  "Enable, disable, or inspect usage-based overage billing beyond the included seat allowance.",
	}
	parent.AddCommand(ondemandEnableCmd(a))
	parent.AddCommand(ondemandDisableCmd(a))
	parent.AddCommand(ondemandStatusCmd(a))
	return parent
}

func ondemandEnableCmd(a *app.App) *cobra.Command {
	var acceptTerms bool
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable on-demand (overage) billing",
		Long: "Opt in to usage-based overage billing beyond the included seat allowance. Requires an " +
			"active paid subscription with a payment method on file, and acceptance of the on-demand terms.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !acceptTerms {
				tui.Warning("Enabling on-demand billing opts you in to usage-based charges beyond your seat allowance.")
				tui.Info("Terms: %s", termsURL)
				return errors.New("re-run with --accept-terms to confirm")
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			state, err := NewService(client.Connection()).EnableOnDemand(cmd.Context(), termsVersion)
			if err != nil {
				return err
			}
			tui.Success("On-demand billing enabled (terms %s accepted).", termsVersion)
			return a.Output.Print(&ondemandResult{state: state})
		},
	}
	cmd.Flags().BoolVar(&acceptTerms, "accept-terms", false, "accept the on-demand billing terms ("+termsURL+")")
	return cmd
}

func ondemandDisableCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable on-demand (overage) billing",
		Long:  "Opt out of usage-based overage billing. Included seat limits continue to apply.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			state, err := NewService(client.Connection()).DisableOnDemand(cmd.Context())
			if err != nil {
				return err
			}
			tui.Success("On-demand billing disabled. Seat limits now apply.")
			return a.Output.Print(&ondemandResult{state: state})
		},
	}
}

func ondemandStatusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show on-demand billing state",
		Long:  "Show the tenant account's on-demand billing state: opt-in, payment method, and dunning posture.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			state, err := NewService(client.Connection()).OnDemandState(cmd.Context())
			if err != nil {
				return err
			}
			return a.Output.Print(&ondemandResult{state: state})
		},
	}
}

type ondemandResult struct{ state *OnDemandState }

func (r *ondemandResult) RenderJSON() ([]byte, error) {
	usage := make([]map[string]any, 0, len(r.state.Usage))
	for _, u := range r.state.Usage {
		m := map[string]any{
			"feature_key":        u.FeatureKey,
			"display_name":       u.DisplayName,
			"unit_label":         u.UnitLabel,
			"included_limit":     u.IncludedLimit,
			"consumed":           u.Consumed,
			"seats":              u.Seats,
			"tier":               u.Tier,
			"provisional":        u.Provisional,
			"overage_used":       u.OverageUsed,
			"overage_used_minor": u.OverageUsedMinor,
			"settlement_pending": u.SettlementPending,
		}
		if !u.PeriodEnd.IsZero() {
			m["period_end"] = u.PeriodEnd.Format(time.RFC3339)
		}
		if u.Overage != nil {
			m["overage"] = map[string]any{
				"cap_kind":         u.Overage.CapKind,
				"cap_units":        u.Overage.CapUnits,
				"unit_price_minor": u.Overage.UnitPriceMinor,
				"cap_amount_minor": u.Overage.CapAmountMinor,
				"currency":         u.Overage.Currency,
			}
		}
		usage = append(usage, m)
	}
	return json.MarshalIndent(map[string]any{
		"enabled":                r.state.Enabled,
		"payment_method_on_file": r.state.PaymentMethodOnFile,
		"payment_posture":        r.state.Posture,
		"feature_usage":          usage,
	}, "", "  ")
}

func (r *ondemandResult) RenderPlain() string {
	lines := []string{
		"enabled\t" + boolText(r.state.Enabled),
		"payment_method\t" + boolText(r.state.PaymentMethodOnFile),
		"posture\t" + r.state.Posture,
	}
	for _, u := range r.state.Usage {
		lines = append(lines, fmt.Sprintf("usage.%s\t%s", u.FeatureKey, plainUsage(u)))
		if u.Overage != nil && u.OverageUsed > 0 {
			lines = append(lines, fmt.Sprintf("overage.%s\t%s", u.FeatureKey, plainOverage(u)))
		}
	}
	return strings.Join(lines, "\n")
}

func (r *ondemandResult) RenderTable() string {
	p := panel.New("On-demand billing").
		Field("Enabled", enabledText(r.state.Enabled)).
		Field("Payment method", onFileText(r.state.PaymentMethodOnFile)).
		Field("Payment posture", r.state.Posture).
		Render()

	blocks := []string{p}
	for i := range r.state.Usage {
		blocks = append(blocks, renderFeatureUsage(r.state.Usage[i], r.state.Enabled))
	}
	return section.Join(blocks...)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func enabledText(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func onFileText(b bool) string {
	if b {
		return "on file"
	}
	return "none"
}
