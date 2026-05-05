package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	cliauth "github.com/safedep/cli/internal/auth"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/output"
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

// statusResult adapts cliauth.Status to the output.Renderer interface.
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

func (r *statusResult) AsJSON() (any, error) {
	out := statusJSON{
		Profile:    r.st.Profile,
		Tenant:     r.st.Tenant,
		APIKey:     r.st.APIKey,
		OAuthToken: r.st.OAuth,
	}
	if !r.st.OAuthExpiresAt.IsZero() {
		out.OAuthExpiresAt = r.st.OAuthExpiresAt.UTC().Format(time.RFC3339)
	}
	return out, nil
}

func (r *statusResult) Render(_ tui.Theme, mode output.Mode) string {
	switch mode {
	case output.Agent:
		return r.renderAgent()
	case output.Plain:
		return r.renderPlain()
	default:
		return r.renderRich()
	}
}

func (r *statusResult) renderAgent() string {
	tenant := r.st.Tenant
	if tenant == "" {
		tenant = "-"
	}
	exp := "-"
	if !r.st.OAuthExpiresAt.IsZero() {
		exp = r.st.OAuthExpiresAt.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("profile=%s tenant=%s api_key=%t oauth=%t oauth_exp=%s",
		r.st.Profile, tenant, r.st.APIKey, r.st.OAuth, exp)
}

func (r *statusResult) renderPlain() string {
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

func (r *statusResult) renderRich() string {
	rows := [][2]string{
		{"Profile", r.st.Profile},
		{"Tenant", emptyDash(r.st.Tenant)},
		{"API key", yesNoBadge(r.st.APIKey)},
		{"OAuth token", yesNoBadge(r.st.OAuth)},
	}
	if !r.st.OAuthExpiresAt.IsZero() {
		rows = append(rows, [2]string{"OAuth expires", r.st.OAuthExpiresAt.UTC().Format(time.RFC3339)})
	}

	var sb strings.Builder
	sb.WriteString("Authentication\n")
	for _, row := range rows {
		fmt.Fprintf(&sb, "  %-15s %s\n", row[0]+":", row[1])
	}
	return strings.TrimRight(sb.String(), "\n")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func yesNoBadge(b bool) string {
	if b {
		return tui.Badge(theme.RoleSuccess, "configured")
	}
	return tui.Badge(theme.RoleWarning, "not configured")
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
