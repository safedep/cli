package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/table"
	"github.com/safedep/dry/tui/theme"
	"github.com/spf13/cobra"
)

const (
	statusActive        = "active"
	statusActiveTrial   = "active-trial"
	statusFree          = "free"
	statusPastDue       = "past-due"
	statusPendingCancel = "active-pending-cancellation"
)

func statusCmd(a *app.App) *cobra.Command {
	var showEntitlements bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show subscription status",
		Long:  "Show the tenant account's subscription status, tier, trial, and on-demand billing. Pass --entitlements to also list the account's entitlements.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			acct, err := runStatus(cmd.Context(), NewService(client.Connection()))
			if err != nil {
				return err
			}
			return a.Output.Print(&statusResult{acct: acct, showEntitlements: showEntitlements})
		},
	}
	cmd.Flags().BoolVar(&showEntitlements, "entitlements", false, "also list the account's entitlements")
	return cmd
}

func runStatus(ctx context.Context, svc StatusGetter) (*AccountStatus, error) {
	return svc.Status(ctx)
}

func statusBadge(s string) string {
	var role theme.Role
	switch s {
	case statusActive:
		role = theme.RoleSuccess
	case statusActiveTrial:
		role = theme.RoleInfo
	case statusPastDue, statusPendingCancel:
		role = theme.RoleWarning
	default:
		role = theme.RoleMuted
	}
	return tui.Badge(role, strings.ToUpper(s))
}

// nextStepHint returns the single most useful next action for a status, or
// "" when the account needs nothing.
func nextStepHint(acct *AccountStatus) string {
	switch acct.Status {
	case statusFree:
		return "No active subscription. Start a free trial: safedep subscription trial enable"
	case statusActiveTrial:
		return "Trial active. Subscribe anytime: safedep subscription create"
	case statusPastDue:
		return "Payment past due. Update billing: safedep subscription portal open"
	default:
		return "Manage billing: safedep subscription portal open"
	}
}

func onDemandSummary(s *OnDemandState) string {
	if s == nil {
		return "unknown"
	}
	if !s.Enabled {
		return "disabled"
	}
	detail := "no payment method"
	if s.PaymentMethodOnFile {
		detail = "payment method on file"
	}
	return fmt.Sprintf("enabled (%s, %s)", detail, s.Posture)
}

type statusResult struct {
	acct             *AccountStatus
	showEntitlements bool
}

func (r *statusResult) RenderJSON() ([]byte, error) {
	out := map[string]any{
		"status":   r.acct.Status,
		"tier":     r.acct.Tier,
		"interval": r.acct.Interval,
		// Always a list, never null, so consumers can iterate unconditionally.
		"add_ons": append([]string{}, r.acct.ActiveAddOns...),
	}
	if r.acct.Seats > 0 {
		out["seats"] = r.acct.Seats
	}
	if r.showEntitlements {
		out["entitlements"] = r.acct.Entitlements
	}
	if r.acct.Trial != nil {
		out["trial"] = map[string]any{
			"days_remaining": r.acct.Trial.DaysRemaining,
			"expires_at":     r.acct.Trial.ExpiresAt.UTC().Format("2006-01-02"),
		}
	}
	if r.acct.OnDemand != nil {
		out["on_demand"] = map[string]any{
			"enabled":                r.acct.OnDemand.Enabled,
			"payment_method_on_file": r.acct.OnDemand.PaymentMethodOnFile,
			"payment_posture":        r.acct.OnDemand.Posture,
		}
	}
	if eu := r.acct.Endpoints; eu != nil {
		// Emit [] rather than null when the server sends no breakdown rows,
		// so consumers can iterate unconditionally.
		breakdown := []map[string]any{}
		for _, row := range eu.Breakdown {
			breakdown = append(breakdown, map[string]any{
				"class":           row.DisplayName,
				"active_assets":   row.ActiveAssets,
				"assets_per_unit": row.AssetsPerUnit,
				"units":           row.Units,
			})
		}
		endpoints := map[string]any{
			"units_used": eu.UnitsUsed,
			"breakdown":  breakdown,
		}
		if eu.HasIncluded {
			endpoints["units_included"] = eu.UnitsIncluded
		}
		if !eu.PeriodEnd.IsZero() {
			endpoints["resets_at"] = eu.PeriodEnd.UTC().Format("2006-01-02")
		}
		out["endpoint_usage"] = endpoints
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *statusResult) RenderPlain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "status\t%s\ntier\t%s\n", r.acct.Status, dashEmpty(r.acct.Tier))
	if r.acct.Seats > 0 {
		fmt.Fprintf(&b, "seats\t%d\n", r.acct.Seats)
	}
	fmt.Fprintf(&b, "interval\t%s\n", dashEmpty(r.acct.Interval))
	fmt.Fprintf(&b, "add_ons\t%s\n", dashEmpty(strings.Join(r.acct.ActiveAddOns, ",")))
	if r.acct.Trial != nil {
		fmt.Fprintf(&b, "trial_days_remaining\t%d\n", r.acct.Trial.DaysRemaining)
	}
	fmt.Fprintf(&b, "on_demand\t%s\n", onDemandSummary(r.acct.OnDemand))
	if eu := r.acct.Endpoints; eu != nil {
		if eu.HasIncluded {
			fmt.Fprintf(&b, "endpoint_units\t%d / %d\n", eu.UnitsUsed, eu.UnitsIncluded)
		} else {
			fmt.Fprintf(&b, "endpoint_units\t%d\n", eu.UnitsUsed)
		}
		for _, row := range eu.Breakdown {
			fmt.Fprintf(&b, "endpoint_class\t%s active=%d per_unit=%d units=%d\n",
				row.DisplayName, row.ActiveAssets, row.AssetsPerUnit, row.Units)
		}
	}
	if r.showEntitlements {
		for _, e := range r.acct.Entitlements {
			fmt.Fprintf(&b, "entitlement\t%s\n", e)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *statusResult) RenderTable() string {
	p := panel.New("Subscription").
		Field("Status", statusBadge(r.acct.Status)).
		Field("Tier", dashEmpty(titleCase(r.acct.Tier)))
	if r.acct.Seats > 0 {
		p = p.Field("Seats", fmt.Sprintf("%d", r.acct.Seats))
	}
	if r.acct.Interval != "" {
		p = p.Field("Billing", titleCase(r.acct.Interval))
	}
	if r.acct.Trial != nil {
		p = p.Field("Trial ends", fmt.Sprintf("in %d days (%s)", r.acct.Trial.DaysRemaining, r.acct.Trial.ExpiresAt.Format("2006-01-02")))
	}
	p = p.Field("On-demand", onDemandSummary(r.acct.OnDemand))

	parts := []string{p.Render()}
	if len(r.acct.ActiveAddOns) > 0 {
		rows := make([][]string, 0, len(r.acct.ActiveAddOns))
		for _, a := range r.acct.ActiveAddOns {
			rows = append(rows, []string{a})
		}
		parts = append(parts, table.New().Title("Add-ons").Headers("Add-on").Rows(rows...).Render())
	}
	if r.acct.Endpoints != nil {
		parts = append(parts, renderEndpointUsage(r.acct.Endpoints))
	}
	if r.showEntitlements && len(r.acct.Entitlements) > 0 {
		rows := make([][]string, 0, len(r.acct.Entitlements))
		for _, e := range r.acct.Entitlements {
			rows = append(rows, []string{e})
		}
		parts = append(parts, table.New().Title("Entitlements").Headers("Feature").Rows(rows...).Render())
	}
	if hint := usageAlertHint(r.acct); hint != "" {
		parts = append(parts, section.Hint(hint))
	}
	if hint := nextStepHint(r.acct); hint != "" {
		parts = append(parts, section.Hint(hint))
	}
	return section.Join(parts...)
}

// usageAlertHint nudges the user toward the usage report only when overage
// needs attention: a charge is accruing, still settling, or capped. The quiet
// case stays quiet so the fast status path carries no usage noise.
func usageAlertHint(acct *AccountStatus) string {
	if acct.OnDemand == nil {
		return ""
	}
	for _, fu := range acct.OnDemand.Usage {
		if fu.OverageUsed > 0 || fu.OverageUsedMinor > 0 || fu.SettlementPending || capReached(fu) {
			return "Your account has overage usage. Usage report: safedep subscription ondemand status"
		}
	}
	return ""
}

func dashEmpty(s string) string {
	if s == "" || s == "unknown" {
		return "-"
	}
	return s
}

// titleCase uppercases the first rune of an ASCII token (e.g. "team" ->
// "Team"). Enough for tier display; avoids deprecated strings.Title.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
