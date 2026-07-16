package auth

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	cliauth "github.com/safedep/cli/internal/auth"
	drytui "github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/theme"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status for the active profile",
		Long:  "Report whether the active profile holds valid credentials and which tenant they bind to.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := cliauth.BuildStatus(cmd.Context(), a.Profile(), a.KeychainOptions())
			if err != nil {
				return err
			}
			return a.Output.Print(&statusResult{st: st})
		},
	}
}

type statusResult struct {
	st cliauth.Status
}

type statusJSON struct {
	Profile        string `json:"profile"`
	Tenant         string `json:"tenant,omitempty"`
	APIKey         bool   `json:"api_key_present"`
	OAuthToken     bool   `json:"oauth_token_present"`
	OAuthExpiresAt string `json:"oauth_expires_at,omitempty"`
}

func (r *statusResult) RenderJSON() ([]byte, error) {
	out := statusJSON{
		Profile:    r.st.Profile,
		Tenant:     r.st.Tenant,
		APIKey:     r.st.APIKey,
		OAuthToken: r.st.OAuth,
	}
	if !r.st.OAuthExpiresAt.IsZero() {
		out.OAuthExpiresAt = r.st.OAuthExpiresAt.UTC().Format(time.RFC3339)
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *statusResult) RenderPlain() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Profile:        %s\n", r.st.Profile)
	if r.st.Tenant != "" {
		fmt.Fprintf(&sb, "Tenant:         %s\n", r.st.Tenant)
	}
	fmt.Fprintf(&sb, "API key:        %s\n", yesNo(r.st.APIKey))
	fmt.Fprintf(&sb, "OAuth token:    %s\n", yesNo(r.st.OAuth))
	if !r.st.OAuthExpiresAt.IsZero() {
		fmt.Fprintf(&sb, "OAuth expires:  %s\n", r.st.OAuthExpiresAt.UTC().Format(time.RFC3339))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *statusResult) RenderTable() string {
	now := time.Now()
	expiryLabel := "OAuth expires"
	if r.st.OAuth && !r.st.OAuthValid(now) {
		expiryLabel = "OAuth expired"
	}
	card := panel.New("Authentication").
		Field("Status", overallStatusBadge(r.st, now)).
		Field("Profile", r.st.Profile).
		Field("Tenant", emptyDash(r.st.Tenant)).
		Field("API key", yesNoBadge(r.st.APIKey)).
		Field("OAuth token", oauthBadge(r.st, now)).
		FieldIf(!r.st.OAuthExpiresAt.IsZero(), expiryLabel,
			humanize.Time(r.st.OAuthExpiresAt, now)).
		Render()
	if hint := nextStepHint(r.st, now); hint != "" {
		return section.Join(card, section.Hint(hint))
	}
	return card
}

// overallStatusBadge summarises the credential state in one badge so users
// do not have to reason over the per-credential rows. An expired OAuth
// token does not count as authenticated.
func overallStatusBadge(st cliauth.Status, now time.Time) string {
	switch {
	case st.APIKey && st.OAuthValid(now):
		return drytui.Badge(theme.RoleSuccess, "authenticated")
	case st.APIKey || st.OAuth:
		return drytui.Badge(theme.RoleMedium, "partially authenticated")
	default:
		return drytui.Badge(theme.RoleWarning, "not authenticated")
	}
}

func oauthBadge(st cliauth.Status, now time.Time) string {
	switch {
	case !st.OAuth:
		return drytui.Badge(theme.RoleWarning, "not configured")
	case !st.OAuthValid(now):
		return drytui.Badge(theme.RoleError, "expired")
	default:
		return drytui.Badge(theme.RoleSuccess, "configured")
	}
}

// nextStepHint returns table-mode guidance for reaching a fully
// authenticated state. Empty when all credentials are present and valid.
func nextStepHint(st cliauth.Status, now time.Time) string {
	switch {
	case !st.APIKey && !st.OAuth:
		return "Run 'safedep auth login' to authenticate with SafeDep Cloud."
	case st.OAuth && !st.OAuthValid(now):
		return "OAuth token expired. Run 'safedep auth login' to re-authenticate."
	case !st.APIKey:
		return "Data plane API key missing. Run 'safedep auth login' to create one."
	case !st.OAuth:
		return "Control plane OAuth token missing. Run 'safedep auth login' to complete setup."
	}
	return ""
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func yesNoBadge(b bool) string {
	if b {
		return drytui.Badge(theme.RoleSuccess, "configured")
	}
	return drytui.Badge(theme.RoleWarning, "not configured")
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
