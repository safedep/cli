package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/safedep/cli/internal/agent"
	"github.com/safedep/cli/internal/app"
	drytui "github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/table"
	"github.com/safedep/dry/tui/theme"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	var flags statusInput

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show SafeDep MCP integration status for AI agents",
		Long: "Report, for every supported AI coding agent, whether it is detected on this " +
			"machine and whether the SafeDep MCP server is configured in its config file. " +
			"Does not require authentication. Pass --workspace to also inspect workspace-level config.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := newMCPService(agent.NewRegistry(), nil)

			// status accumulates per-agent probe errors alongside a partial
			// report. Print the report first so healthy agents are still shown,
			// then surface the errors via stderr and a non-zero exit.
			statuses, statusErr := svc.status(flags)
			printErr := a.Output.Print(&statusResult{
				statuses:     statuses,
				workspaceDir: flags.WorkspaceDir,
			})

			return errors.Join(statusErr, printErr)
		},
	}

	cmd.Flags().StringVar(&flags.WorkspaceDir, "workspace", "", "project directory for workspace-level status (empty = skip)")

	return cmd
}

type statusResult struct {
	statuses     []agentStatus
	workspaceDir string
}

type scopeJSON struct {
	Supported  bool   `json:"supported"`
	Configured bool   `json:"configured"`
	Path       string `json:"path,omitempty"`
	Error      string `json:"error,omitempty"`
}

func toScopeJSON(sc scopeStatus) scopeJSON {
	out := scopeJSON{
		Supported:  sc.Supported,
		Configured: sc.Configured,
		Path:       sc.Path,
	}
	if sc.Err != nil {
		out.Error = sc.Err.Error()
	}
	return out
}

type agentStatusJSON struct {
	Name      string     `json:"name"`
	Detected  bool       `json:"detected"`
	Global    scopeJSON  `json:"global"`
	Workspace *scopeJSON `json:"workspace,omitempty"`
}

func (r *statusResult) RenderJSON() ([]byte, error) {
	out := make([]agentStatusJSON, 0, len(r.statuses))
	for _, st := range r.statuses {
		row := agentStatusJSON{
			Name:     st.Name,
			Detected: st.Detected,
			Global:   toScopeJSON(st.Global),
		}
		if r.workspaceDir != "" {
			ws := toScopeJSON(st.Workspace)
			row.Workspace = &ws
		}
		out = append(out, row)
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *statusResult) RenderPlain() string {
	var sb strings.Builder
	for _, st := range r.statuses {
		fmt.Fprintf(&sb, "%s\tdetected=%s\tglobal=%s",
			st.Name, yesNo(st.Detected), scopePlain(st.Detected, st.Global))
		if r.workspaceDir != "" {
			fmt.Fprintf(&sb, "\tworkspace=%s", scopePlain(st.Detected, st.Workspace))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *statusResult) RenderTable() string {
	withWorkspace := r.workspaceDir != ""

	headers := []string{"Agent", "Detected", "Global"}
	if withWorkspace {
		headers = append(headers, "Workspace")
	}

	t := table.New().Headers(headers...)
	for _, st := range r.statuses {
		row := []string{st.Name, detectedBadge(st.Detected), scopeBadge(st.Detected, st.Global)}
		if withWorkspace {
			row = append(row, scopeBadge(st.Detected, st.Workspace))
		}
		t = t.Row(row...)
	}
	return t.Render()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func detectedBadge(detected bool) string {
	if detected {
		return drytui.Badge(theme.RoleSuccess, "yes")
	}
	return drytui.Badge(theme.RoleMuted, "no")
}

// scopeBadge renders a styled cell for a config scope. Undetected agents and
// unsupported scopes carry no SafeDep state, so they render as a plain dash. A
// failed probe renders as "error" rather than "not configured", which would
// misreport an unreadable config as a clean absence.
func scopeBadge(detected bool, sc scopeStatus) string {
	if !detected || !sc.Supported {
		return "-"
	}
	if sc.Err != nil {
		return drytui.Badge(theme.RoleError, "error")
	}
	if sc.Configured {
		return drytui.Badge(theme.RoleSuccess, "configured")
	}
	return drytui.Badge(theme.RoleWarning, "not configured")
}

func scopePlain(detected bool, sc scopeStatus) string {
	if !detected || !sc.Supported {
		return "-"
	}
	if sc.Err != nil {
		return "error"
	}
	if sc.Configured {
		return "configured"
	}
	return "not configured"
}
