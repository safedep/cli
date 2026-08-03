package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/meter"
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
		lines = append(lines, fmt.Sprintf("usage.%s\t%d/%d", u.FeatureKey, u.Consumed, u.IncludedLimit))
		if u.Overage != nil && u.OverageUsed > 0 {
			lines = append(lines, fmt.Sprintf("overage.%s\t%d", u.FeatureKey, u.OverageUsed))
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

// renderFeatureUsage renders one feature's context line, its bar(s), and an
// optional next-step hint. Presentation only: it branches on the numbers the
// server already computed and never derives billing values.
func renderFeatureUsage(fu FeatureUsage, enabled bool) string {
	lines := []string{usageContextLine(fu)}

	switch {
	case fu.IncludedLimit == 0: // not available on this tier
		lines = append(lines, section.Hint("Not available on your plan"))
	case fu.IncludedLimit < 0: // unlimited
		lines = append(lines, "Included   Unlimited")
	default:
		bars := []meter.Bar{includedBar(fu)}
		if ob, ok := overageBar(fu, enabled); ok {
			bars = append(bars, ob)
		}
		lines = append(lines, meter.Render(bars...))
	}

	if hint := usageHint(fu, enabled); hint != "" {
		lines = append(lines, section.Hint(hint))
	}
	return strings.Join(lines, "\n")
}

func usageContextLine(fu FeatureUsage) string {
	period := "this month"
	if !fu.PeriodEnd.IsZero() {
		period = fmt.Sprintf("this month (resets %s)", fu.PeriodEnd.Format("2 Jan"))
	}

	seats := fmt.Sprintf("%d seat", fu.Seats)
	if fu.Seats != 1 {
		seats += "s"
	}

	var allowance string
	switch {
	case fu.IncludedLimit < 0:
		allowance = "unlimited"
	case fu.IncludedLimit == 0:
		allowance = "not available"
	default:
		allowance = fmt.Sprintf("%d %s/mo", fu.IncludedLimit, fu.UnitLabel)
	}

	line := fmt.Sprintf("%s: %s, %s, %s", fu.DisplayName, period, allowance, seats)
	if fu.Provisional {
		line += " (preview: not enforced)"
	}
	return line
}

func includedBar(fu FeatureUsage) meter.Bar {
	reading := "full"
	if fu.Consumed < fu.IncludedLimit {
		reading = fmt.Sprintf("%d%%", percentInt(fu.Consumed, fu.IncludedLimit))
	}
	label := "Included"
	if fu.Provisional {
		label = "Used"
	}
	return meter.Bar{
		Label:     label,
		Value:     fu.Consumed,
		Max:       fu.IncludedLimit,
		ValueText: fmt.Sprintf("%d / %d %s (%s)", fu.Consumed, fu.IncludedLimit, fu.UnitLabel, reading),
		Warn:      !fu.Provisional && fu.Consumed >= fu.IncludedLimit,
	}
}

// overageBar is shown when there is accrued overage to display, or when
// on-demand is enabled and the included allowance is exhausted. Keying on
// OverageUsed (not Enabled alone) keeps an accrued charge visible while it is
// still settling after on-demand is disabled.
func overageBar(fu FeatureUsage, enabled bool) (meter.Bar, bool) {
	ov := fu.Overage
	if ov == nil {
		return meter.Bar{}, false
	}
	show := fu.OverageUsed > 0 || (enabled && fu.Consumed >= fu.IncludedLimit)
	if !show {
		return meter.Bar{}, false
	}

	if ov.CapKind == "monetary" {
		text := fmt.Sprintf("%s of %s cap", money(fu.OverageUsedMinor, ov.Currency), money(ov.CapAmountMinor, ov.Currency))
		if fu.SettlementPending {
			text += " (settling)"
		}
		return meter.Bar{
			Label:     "Overage",
			Value:     fu.OverageUsedMinor,
			Max:       ov.CapAmountMinor,
			ValueText: text,
			Warn:      ov.CapAmountMinor > 0 && fu.OverageUsedMinor >= ov.CapAmountMinor,
		}, true
	}

	text := fmt.Sprintf("%d / %d %s", fu.OverageUsed, ov.CapUnits, fu.UnitLabel)
	if fu.SettlementPending {
		text += " (settling)"
	}
	return meter.Bar{
		Label:     "Overage",
		Value:     fu.OverageUsed,
		Max:       ov.CapUnits,
		ValueText: text,
		Warn:      ov.CapUnits > 0 && fu.OverageUsed >= ov.CapUnits,
	}, true
}

func usageHint(fu FeatureUsage, enabled bool) string {
	if fu.Overage == nil {
		return ""
	}
	switch {
	case fu.SettlementPending:
		return "Accrued overage is still being billed"
	case fu.Overage.CapKind == "monetary" && fu.Overage.CapAmountMinor > 0 && fu.OverageUsedMinor >= fu.Overage.CapAmountMinor:
		return "Cap reached: contact SafeDep to raise your cap"
	case !enabled && fu.Consumed >= fu.IncludedLimit:
		return "Enable on-demand to continue: safedep subscription ondemand enable"
	default:
		return ""
	}
}

// money formats minor currency units (e.g. cents) as a display amount.
func money(minor int64, currency string) string {
	symbol := "$"
	if currency != "" && currency != "usd" {
		symbol = strings.ToUpper(currency) + " "
	}
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%s%d.%02d", sign, symbol, minor/100, minor%100)
}

func percentInt(value, max int64) int {
	if max <= 0 || value <= 0 {
		return 0
	}
	if value >= max {
		return 100
	}
	return int((value * 100) / max)
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
