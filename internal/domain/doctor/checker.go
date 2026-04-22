package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/safedep/cli/internal/protect/mcp/adapter"
	"github.com/safedep/dry/tui/table"
)

type checkStatus string

const (
	statusOK      checkStatus = "ok"
	statusWarning checkStatus = "warning"
	statusInfo    checkStatus = "info"
)

type CheckEntry struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
}

// CheckResult holds the full diagnostic report. Implements output.Renderable.
type CheckResult struct {
	Entries []CheckEntry `json:"checks"`
	AllOK   bool         `json:"all_ok"`
}

func (r *CheckResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *CheckResult) RenderTable() string {
	t := table.New().Headers("Check", "Status", "Detail")
	for _, e := range r.Entries {
		t.Row(e.Name, string(e.Status), e.Detail)
	}
	return t.Render()
}

func (r *CheckResult) RenderPlain() string {
	var b strings.Builder
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "%-30s %-10s %s\n", e.Name, e.Status, e.Detail)
	}
	return b.String()
}

func (r *CheckResult) add(status checkStatus, name, detail string) {
	r.Entries = append(r.Entries, CheckEntry{Name: name, Status: status, Detail: detail})
	if status == statusWarning {
		r.AllOK = false
	}
}

// CheckInput is everything the Checker needs; the cmd layer resolves each field from App.
type CheckInput struct {
	Authenticated bool
	Tenant        string
	MCPAdapters   []adapter.MCPAdapter
	OptionalTools []string
}

// Checker runs all diagnostic checks and returns a structured result.
// Note: all checks run before output is rendered — no streaming progress.
type Checker struct{}

func (c *Checker) Check(ctx context.Context, in CheckInput) *CheckResult {
	result := &CheckResult{AllOK: true}

	if !in.Authenticated {
		result.add(statusWarning, "auth", "not authenticated — run `safedep auth login`")
	} else {
		result.add(statusOK, "auth", "credentials found")
	}

	if in.Tenant == "" {
		result.add(statusWarning, "config", "tenant not set in ~/.config/safedep/config.toml")
	} else {
		result.add(statusOK, "config", fmt.Sprintf("tenant = %s", in.Tenant))
	}

	for _, ad := range in.MCPAdapters {
		label := fmt.Sprintf("protect/mcp/%s", ad.Name())

		detection, err := ad.Detect(ctx)
		if err != nil || !detection.Found {
			result.add(statusInfo, label, "not detected")
			continue
		}

		st, err := ad.Status(ctx)
		if err != nil {
			result.add(statusWarning, label, fmt.Sprintf("status error: %v", err))
			continue
		}

		if !st.Installed {
			result.add(statusWarning, label, "detected but SafeDep MCP not configured — run `safedep protect mcp install`")
		} else {
			result.add(statusOK, label, fmt.Sprintf("configured (%s)", st.ConfigPath))
		}
	}

	for _, tool := range in.OptionalTools {
		label := fmt.Sprintf("tools/%s", tool)
		if path, err := exec.LookPath(tool); err == nil {
			result.add(statusOK, label, path)
		} else {
			result.add(statusInfo, label, "not found on PATH (optional)")
		}
	}

	return result
}
