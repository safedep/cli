package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

func statusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show MCP configuration status for all detected AI IDEs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return PrintStatus(cmd.Context(), a)
		},
	}
}

// PrintStatus is exported so protect status can call it.
func PrintStatus(ctx context.Context, a *app.App) error {
	adapters := adapter.All()
	result := &mcpStatusResult{}

	for _, ad := range adapters {
		detection, err := ad.Detect(ctx)
		if err != nil || !detection.Found {
			continue
		}

		st, err := ad.Status(ctx)
		entry := adapterStatusEntry{
			Name:       ad.DisplayName(),
			ConfigPath: detection.ConfigPath,
		}

		switch {
		case err != nil:
			entry.State = "error"
		case st.Installed && st.Valid:
			entry.State = "configured"
		case st.Installed:
			entry.State = "stale"
		default:
			entry.State = "not installed"
		}

		result.Adapters = append(result.Adapters, entry)
	}

	if len(result.Adapters) == 0 {
		a.Output.Info("No supported AI IDEs detected.")
		return nil
	}

	return a.Output.Print(result)
}

type adapterStatusEntry struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	ConfigPath string `json:"config_path"`
}

type mcpStatusResult struct {
	Adapters []adapterStatusEntry `json:"adapters"`
}

func (r *mcpStatusResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *mcpStatusResult) RenderTable() string {
	t := table.New().Headers("IDE", "State", "Config Path")
	for _, a := range r.Adapters {
		t.Row(a.Name, a.State, a.ConfigPath)
	}
	return t.Render()
}

func (r *mcpStatusResult) RenderPlain() string {
	out := ""
	for _, a := range r.Adapters {
		out += fmt.Sprintf("%-20s %-15s %s\n", a.Name, a.State, a.ConfigPath)
	}
	return out
}
