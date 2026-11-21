package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/dry/tui/table"

	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

// A width of 0 or less means that stdout is not a terminal. The view then
// does not wrap, so a file or a pipe gets each value on one line.
const (
	// columnGap is the gap of cloudpolicy.KeyValues.
	columnGap = "  "
	// minWrapWidth keeps a wrapped value readable on a very narrow terminal.
	// Below it, a line runs past the terminal edge instead.
	minWrapWidth = 20
	// listSeparator joins the items of a list value, as FieldValue.Human
	// writes them.
	listSeparator = ", "
)

// overridesTable lists the policy overrides. The description is free text,
// so it wraps to the width that the other columns leave.
func (r *showResult) overridesTable(now time.Time, width int) string {
	rows := make([][]string, 0, len(r.Overrides))
	for _, o := range r.Overrides {
		expires := "-"
		if o.expires != nil {
			expires = humanTimeCell(o.expires, now)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", o.Priority), o.Name, o.Selector, expires, cloudpolicy.EmptyDash(o.Description),
		})
	}
	headers := []string{"PRIORITY", "NAME", "SELECTOR", "EXPIRES", "DESCRIPTION"}
	rows = clitui.EscapeCells(rows)
	wrapLastColumn(headers, rows, width)
	return table.New().
		Title("Overrides (priority, highest first)").
		Headers(headers...).
		Rows(rows...).
		EmptyMessage(wrapText("No policy overrides. The policy base applies to every subject. "+
			"Add one with: safedep policy edit", width)).
		Render()
}

// wrapLastColumn wraps the cells of the last column, so the table fits the
// width. A dry/tui table pads each cell with one space on each side and draws
// one border column between cells and at each edge. dry/tui has no width
// option for a table yet.
func wrapLastColumn(headers []string, rows [][]string, width int) {
	if width <= 0 {
		return
	}
	last := len(headers) - 1
	used := 1
	for col := range last {
		w := lipgloss.Width(headers[col])
		for _, row := range rows {
			w = max(w, lipgloss.Width(row[col]))
		}
		used += w + 3
	}
	avail := max(width-used-3, lipgloss.Width(headers[last]))
	for _, row := range rows {
		row[last] = strings.Join(wrapValue(row[last], " ", avail), "\n")
	}
}

// wrapRows wraps the values of label and value rows to the width that the
// label column leaves. Each extra line is a row with an empty label, which
// cloudpolicy.KeyValues writes as a continuation of the row above. A value
// that is a summary line breaks after a separator first.
func wrapRows(rows [][2]string, width int) [][2]string {
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, lipgloss.Width(r[0]))
	}
	valueW := 0
	if width > 0 {
		valueW = max(width-labelW-len(columnGap), minWrapWidth)
	}
	out := make([][2]string, 0, len(rows))
	for _, r := range rows {
		label := r[0]
		for _, line := range wrapValue(clitui.EscapeLine(r[1]), sep, valueW) {
			out = append(out, [2]string{label, line})
			label = ""
		}
	}
	return out
}

// wrapText wraps sentences to the width. It breaks after a full stop
// first, so a sentence with a command stays on one line when it fits.
func wrapText(text string, width int) string {
	return strings.Join(wrapValue(text, ". ", width), "\n")
}

// wrapValue splits value into lines of at most width display columns. It
// breaks after a separator first, so a list item or a part of a summary line
// stays whole, and the line keeps the separator mark, as "GPL-2.0,". A part
// wider than width breaks at its spaces. A word wider than width gets a line
// of its own.
func wrapValue(value, separator string, width int) []string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return []string{value}
	}
	mark := strings.TrimRight(separator, " ")
	gap := separator[len(mark):]
	parts := strings.Split(value, separator)
	var lines []string
	line := ""
	for i, part := range parts {
		if i < len(parts)-1 {
			part += mark
		}
		if line != "" && lipgloss.Width(line+gap+part) <= width {
			line += gap + part
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		words := cloudpolicy.WrapWords(part, width)
		if len(words) == 0 {
			line = ""
			continue
		}
		lines = append(lines, words[:len(words)-1]...)
		line = words[len(words)-1]
	}
	if line != "" || len(lines) == 0 {
		lines = append(lines, line)
	}
	return lines
}

// splitPath splits a settings path into the path of its parent message and
// its field name, as "scan_gate.licenses" and "deny".
func splitPath(path string) (string, string) {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}
