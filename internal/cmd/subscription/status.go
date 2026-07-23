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
		"status": r.acct.Status,
		"tier":   r.acct.Tier,
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
	return json.MarshalIndent(out, "", "  ")
}

func (r *statusResult) RenderPlain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "status\t%s\ntier\t%s\n", r.acct.Status, dashEmpty(r.acct.Tier))
	if r.acct.Trial != nil {
		fmt.Fprintf(&b, "trial_days_remaining\t%d\n", r.acct.Trial.DaysRemaining)
	}
	fmt.Fprintf(&b, "on_demand\t%s\n", onDemandSummary(r.acct.OnDemand))
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
	if r.acct.Trial != nil {
		p = p.Field("Trial ends", fmt.Sprintf("in %d days (%s)", r.acct.Trial.DaysRemaining, r.acct.Trial.ExpiresAt.Format("2006-01-02")))
	}
	p = p.Field("On-demand", onDemandSummary(r.acct.OnDemand))

	parts := []string{p.Render()}
	if r.showEntitlements && len(r.acct.Entitlements) > 0 {
		rows := make([][]string, 0, len(r.acct.Entitlements))
		for _, e := range r.acct.Entitlements {
			rows = append(rows, []string{e})
		}
		parts = append(parts, table.New().Title("Entitlements").Headers("Feature").Rows(rows...).Render())
	}
	if hint := nextStepHint(r.acct); hint != "" {
		parts = append(parts, section.Hint(hint))
	}
	return section.Join(parts...)
}

func dashEmpty(s string) string {
	if s == "" || s == "unknown" {
		return "-"
	}
	return s
}

// titleCase uppercases the first rune of an ASCII token (e.g. "professional"
// -> "Professional"). Enough for tier display; avoids deprecated strings.Title.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
