package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/style"
	"github.com/safedep/dry/tui/table"

	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

const (
	// columnGap is the space between the columns of the settings section,
	// the gap of cloudpolicy.KeyValues.
	columnGap = "  "
	// minWrapWidth keeps a wrapped value readable on a very narrow terminal.
	// Below it, a line runs past the terminal edge instead.
	minWrapWidth = 20
	// listSeparator joins the items of a list value, as FieldValue.Human
	// writes them.
	listSeparator = ", "
	notSetValue   = "not set, each tool uses its local value"
	sourceDefault = "default policy"
	sourceBase    = "base"
)

// settingView is one settings path: the value that every subject gets, the
// layer that gives it, and the value of each policy override that sets the
// path. Source is empty when neither the base nor the default policy sets
// the path.
type settingView struct {
	Path      string
	Human     string
	Source    string
	Overrides []settingOverride
}

type settingOverride struct {
	Source string
	Human  string
}

// settingViews lists each path that a layer sets, in contract order. The
// base wins over the default policy. The override values follow the
// override order, from the highest priority to the lowest.
func settingViews(def, base []cloudpolicy.FieldValue, overrides []overrideView) []settingView {
	byPath := map[string]*settingView{}
	get := func(path string) *settingView {
		if v, ok := byPath[path]; ok {
			return v
		}
		v := &settingView{Path: path}
		byPath[path] = v
		return v
	}
	for _, f := range def {
		v := get(f.Path)
		v.Human, v.Source = f.Human, sourceDefault
	}
	for _, f := range base {
		v := get(f.Path)
		v.Human, v.Source = f.Human, sourceBase
	}
	for _, o := range overrides {
		source := fmt.Sprintf("override %q (%d)", o.Name, o.Priority)
		for _, f := range o.settings {
			v := get(f.Path)
			v.Overrides = append(v.Overrides, settingOverride{Source: source, Human: f.Human})
		}
	}
	out := make([]settingView, 0, len(byPath))
	for _, path := range cloudpolicy.LeafPaths() {
		if v, ok := byPath[path]; ok {
			out = append(out, *v)
		}
	}
	return out
}

// settingLine is one row of the settings section. An empty label continues
// the setting above.
type settingLine struct {
	label, source, value string
	// wrapAt is the separator that the value breaks after first.
	wrapAt string
}

// settingsSection groups the settings by the path of their parent message,
// as scan_gate.licenses, with one setting per row. A value that does not fit
// the width wraps after a list item.
func (r *showResult) settingsSection(width int) string {
	settings := settingViews(r.DefaultFields, r.BaseFields, r.Overrides)
	parts := []string{style.Heading("Settings")}
	if r.BaseDescription != "" {
		parts = append(parts, wrapText("Base: "+clitui.EscapeLine(r.BaseDescription), width))
	}
	if len(settings) == 0 {
		parts = append(parts, section.Empty(wrapText("The policy sets no settings. Each tool uses its local values. "+
			"Set them with: safedep policy edit", width)))
		return strings.Join(parts, "\n")
	}

	groups := []string{}
	lines := map[string][]settingLine{}
	hasOverride := false
	for _, s := range settings {
		group, leaf := splitPath(s.Path)
		if _, ok := lines[group]; !ok {
			groups = append(groups, group)
		}
		if group != "" {
			leaf = "  " + leaf
		}
		first := settingLine{label: leaf, source: s.Source, value: s.Human, wrapAt: listSeparator}
		if s.Source == "" {
			first = settingLine{label: leaf, source: "-", value: notSetValue, wrapAt: " "}
		}
		lines[group] = append(lines[group], first)
		for _, o := range s.Overrides {
			lines[group] = append(lines[group], settingLine{source: o.Source, value: o.Human, wrapAt: listSeparator})
			hasOverride = true
		}
	}

	labelW, sourceW := lipgloss.Width("SETTING"), lipgloss.Width("SOURCE")
	for _, group := range groups {
		for _, l := range lines[group] {
			labelW = max(labelW, lipgloss.Width(l.label))
			sourceW = max(sourceW, lipgloss.Width(clitui.EscapeLine(l.source)))
		}
	}
	valueW := max(width-labelW-sourceW-2*len(columnGap), minWrapWidth)

	rows := []string{padRight("SETTING", labelW) + columnGap + padRight("SOURCE", sourceW) + columnGap + "VALUE"}
	for _, group := range groups {
		if group != "" {
			rows = append(rows, group)
		}
		for _, l := range lines[group] {
			label, source := l.label, clitui.EscapeLine(l.source)
			for _, value := range wrapValue(clitui.EscapeLine(l.value), l.wrapAt, valueW) {
				rows = append(rows, padRight(label, labelW)+columnGap+padRight(source, sourceW)+columnGap+value)
				label, source = "", ""
			}
		}
	}
	parts = append(parts, strings.Join(rows, "\n"))
	note := "See the settings document: safedep policy get"
	if hasOverride {
		note = "An override sets its value only for the subjects that its selector matches. " + note
	}
	parts = append(parts, wrapText(note, width))
	return strings.Join(parts, "\n")
}

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
	valueW := max(width-labelW-len(columnGap), minWrapWidth)
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

// wrapValue splits value into lines of at most width display columns. It
// breaks after a separator first, so a list item or a part of a summary line
// stays whole, and the line keeps the separator mark, as "GPL-2.0,". A part
// wider than width breaks at its spaces. A word wider than width gets a line
// of its own, because a broken word reads as two values.
func wrapValue(value, separator string, width int) []string {
	if lipgloss.Width(value) <= width {
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
		words := wrapWords(part, width)
		lines = append(lines, words[:len(words)-1]...)
		line = words[len(words)-1]
	}
	return append(lines, line)
}

// wrapText wraps sentences to the width. It breaks after a full stop
// first, so a sentence with a command stays on one line when it fits.
func wrapText(text string, width int) string {
	return strings.Join(wrapValue(text, ". ", width), "\n")
}

func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for i, word := range strings.Split(text, " ") {
		switch {
		case i == 0:
			line = word
		case lipgloss.Width(line+" "+word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	return append(lines, line)
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

func padRight(s string, width int) string {
	return s + strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
}
