package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/safedep/dry/tui/table"
)

// StatusEntry holds the detected status for one IDE adapter.
type StatusEntry struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	ConfigPath string `json:"config_path"`
}

// StatusResult is the structured output of a status check. Implements output.Renderable.
type StatusResult struct {
	Adapters []StatusEntry `json:"adapters"`
}

func (r *StatusResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *StatusResult) RenderTable() string {
	t := table.New().Headers("IDE", "State", "Config Path")
	for _, a := range r.Adapters {
		t.Row(a.Name, a.State, a.ConfigPath)
	}
	return t.Render()
}

func (r *StatusResult) RenderPlain() string {
	var b strings.Builder
	for _, a := range r.Adapters {
		fmt.Fprintf(&b, "%-20s %-15s %s\n", a.Name, a.State, a.ConfigPath)
	}
	return b.String()
}

// StatusChecker checks MCP configuration state across all provided adapters.
type StatusChecker struct{}

func (c *StatusChecker) Check(ctx context.Context, adapters []adapter.MCPAdapter) (*StatusResult, error) {
	result := &StatusResult{}

	for _, ad := range adapters {
		detection, err := ad.Detect(ctx)
		if err != nil || !detection.Found {
			continue
		}

		st, err := ad.Status(ctx)
		entry := StatusEntry{
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

	return result, nil
}
