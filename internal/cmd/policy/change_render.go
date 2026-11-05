package policy

import (
	"fmt"
	"strings"

	"github.com/safedep/dry/tui/table"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

func (r *changeShowResult) impactTable(p *previewView) string {
	rows := make([][]string, 0, len(p.Coverage))
	for _, c := range p.Coverage {
		what := c.Path
		if c.Value != "" {
			what += " = " + c.Value
		}
		rows = append(rows, []string{what, fmt.Sprint(c.Enforce), fmt.Sprint(c.Partial), fmt.Sprint(c.Detect), fmt.Sprint(c.Unsupported)})
	}
	return table.New().
		Title(fmt.Sprintf("Impact%ssubjects matched: %d %s, %d %s", sep,
			p.MatchedProjects, cloudpolicy.Plural(int(p.MatchedProjects), "project", "projects"),
			p.MatchedEndpoints, cloudpolicy.Plural(int(p.MatchedEndpoints), "endpoint", "endpoints"))).
		Headers("FIELD", "ENFORCED", "PARTIAL", "DETECT ONLY", "NOT SUPPORTED").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No coverage data for the changed fields.").
		Render()
}

func (r *changeShowResult) warningTable(p *previewView) string {
	rows := make([][]string, 0, len(p.Warnings))
	for _, w := range p.Warnings {
		rows = append(rows, []string{w.Code, cloudpolicy.EmptyDash(w.Subject), w.Message})
	}
	return table.New().
		Title("Warnings").
		Headers("CODE", "SUBJECT", "MESSAGE").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No warnings.").
		Render()
}

func directionLine(direction string) string {
	switch direction {
	case "LOOSENS":
		return "LOOSENS. Only an Admin can publish it, and Admins and Owners get a notice"
	case "TIGHTENS":
		return "TIGHTENS. An Admin can publish it"
	default:
		return direction
	}
}

func (r *changeShowResult) RenderPlain() string {
	var b strings.Builder
	c := r.Change
	b.WriteString(strings.Join([]string{
		tui.PlainRow("change_id", c.ID),
		tui.PlainRow("title", c.Title),
		tui.PlainRow("state", c.State),
		tui.PlainRow("base_revision", fmt.Sprint(c.BaseRevision)),
	}, "\n"))
	items := r.ruleOnly
	if p := r.Preview; p != nil {
		fmt.Fprintf(&b, "\nhead_revision\t%d\nstale\t%t\ndirection\t%s", p.HeadRevision, p.Stale, p.Direction)
		items = p.Items
	}
	for _, it := range items {
		b.WriteString("\n" + tui.PlainRow("change", it.Where, it.What, it.Old, it.New, it.Direction))
	}
	if p := r.Preview; p != nil {
		for _, w := range p.Warnings {
			b.WriteString("\n" + tui.PlainRow("warning", w.Code, w.Subject, w.Message))
		}
	}
	return b.String()
}
