package packages

import (
	"fmt"
	"strings"
	"time"

	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/table"
	"github.com/safedep/dry/tui/theme"
)

const (
	verdictMalware      = "malware"
	verdictBenign       = "benign"
	verdictInconclusive = "inconclusive"
)

// verdictBadge renders a verdict as a coloured inline badge. Empty verdict
// (scan not yet completed) renders as a muted dash.
func verdictBadge(verdict string) string {
	if verdict == "" {
		return tui.Badge(theme.RoleMuted, "-")
	}
	return tui.Badge(verdictRole(verdict), strings.ToUpper(verdict))
}

func verdictRole(verdict string) theme.Role {
	switch verdict {
	case verdictMalware:
		return theme.RoleError
	case verdictBenign:
		return theme.RoleSuccess
	case verdictInconclusive:
		return theme.RoleWarning
	default:
		return theme.RoleMuted
	}
}

func confidencePct(c float64) string {
	if c <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", c*100)
}

func packageLabel(s Scan) string {
	return fmt.Sprintf("%s / %s @ %s", s.Ecosystem, s.Name, s.Version)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// scanPanel renders the identity + verdict headline shared by run and get.
func scanPanel(s Scan, title string, now time.Time) string {
	p := panel.New(title).
		Field("Package", packageLabel(s)).
		Field("Verdict", verdictBadge(s.Verdict)).
		Field("Confidence", confidencePct(s.Confidence)).
		Field("Status", s.Status)
	if !s.CompletedAt.IsZero() {
		p = p.Field("Scanned", humanTime(s.CompletedAt, now))
	} else if !s.CreatedAt.IsZero() {
		p = p.Field("Submitted", humanTime(s.CreatedAt, now))
	}
	p = p.FieldIf(s.Failure != "", "Failure", s.Failure).
		Field("Scan ID", s.ScanID)
	return p.Render()
}

// reportBody renders the report sections (inference, evidence, warnings),
// skipping any that are empty so non-library component reports do not show
// hollow tables.
func reportBody(r *Report) string {
	parts := []string{}

	if r.Summary != "" || r.Details != "" {
		body := r.Summary
		if r.Details != "" {
			if body != "" {
				body += "\n\n"
			}
			body += r.Details
		}
		parts = append(parts, section.Titled("Inference", body))
	}

	if len(r.FileEvidences) > 0 {
		rows := make([][]string, 0, len(r.FileEvidences))
		for _, e := range r.FileEvidences {
			loc := e.File
			if e.Line > 0 {
				loc = fmt.Sprintf("%s:%d", e.File, e.Line)
			}
			rows = append(rows, []string{loc, dash(e.Title), dash(e.Confidence), dash(e.Details)})
		}
		parts = append(parts, evidenceTable("File evidence",
			[]string{"Location", "Signal", "Confidence", "Detail"}, rows))
	}

	if len(r.ProjectEvidences) > 0 {
		rows := make([][]string, 0, len(r.ProjectEvidences))
		for _, e := range r.ProjectEvidences {
			rows = append(rows, []string{dash(e.Project), dash(e.Title), dash(e.Confidence), dash(e.Details)})
		}
		parts = append(parts, evidenceTable("Project evidence",
			[]string{"Project", "Signal", "Confidence", "Detail"}, rows))
	}

	if len(r.Warnings) > 0 {
		var b strings.Builder
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		parts = append(parts, section.Titled("Warnings", strings.TrimRight(b.String(), "\n")))
	}

	return section.Join(parts...)
}

// maxEvidenceRows caps how many evidence rows the decorated table view
// renders. Reports can carry a very large file list; the table stays
// readable and points at --output json for the complete set. plain and json
// are never truncated.
const maxEvidenceRows = 20

// evidenceTable renders an evidence table, truncating to maxEvidenceRows
// with a footer that advertises the full count and how to get it.
func evidenceTable(title string, headers []string, rows [][]string) string {
	total := len(rows)
	t := table.New().Title(title).Headers(headers...)
	if total > maxEvidenceRows {
		t = t.Rows(rows[:maxEvidenceRows]...).
			Footer(fmt.Sprintf("showing %d of %d. Use --output json for the full list.", maxEvidenceRows, total))
	} else {
		t = t.Rows(rows...)
	}
	return t.Render()
}

func humanTime(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return humanize.Time(t, now)
}
