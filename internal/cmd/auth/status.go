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
	return panel.New("Authentication").
		Field("Profile", r.st.Profile).
		Field("Tenant", emptyDash(r.st.Tenant)).
		Field("API key", yesNoBadge(r.st.APIKey)).
		Field("OAuth token", yesNoBadge(r.st.OAuth)).
		FieldIf(!r.st.OAuthExpiresAt.IsZero(), "OAuth expires",
			humanize.Time(r.st.OAuthExpiresAt, time.Now())).
		Render()
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
