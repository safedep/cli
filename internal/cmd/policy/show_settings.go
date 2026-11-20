package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/safedep/dry/tui/style"

	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

const (
	notSetSource  = "-"
	notSetValue   = "not set, each tool uses its local value"
	sourceDefault = "default policy"
	sourceBase    = "base"
	// wideSourceIndent starts the source line of a policy override whose
	// source does not fit the SOURCE column, below the setting name.
	wideSourceIndent = "    "
)

// settingView is one settings path: the value that every subject gets, the
// layer that gives it, and the value of each policy override that sets the
// path.
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

// settingViews lists every settings path of the contract, in contract order.
// The base wins over the default policy. That holds because every settings
// path merges by replace (TestLeafPathsMergeByReplace). The override values
// follow the override order, from the highest priority to the lowest, and an
// override past its expiry keeps its row with a mark.
func settingViews(def, base []cloudpolicy.FieldValue, overrides []overrideView, now time.Time) []settingView {
	layers := map[string]settingView{}
	for _, f := range def {
		layers[f.Path] = settingView{Human: f.Human, Source: sourceDefault}
	}
	for _, f := range base {
		layers[f.Path] = settingView{Human: f.Human, Source: sourceBase}
	}
	setBy := map[string][]settingOverride{}
	for _, o := range overrides {
		source := fmt.Sprintf("override %q (%d)", o.Name, o.Priority)
		if o.expires != nil && !o.expires.After(now) {
			source += ", expired"
		}
		for _, f := range o.settings {
			setBy[f.Path] = append(setBy[f.Path], settingOverride{Source: source, Human: f.Human})
		}
	}
	paths := cloudpolicy.LeafPaths()
	out := make([]settingView, 0, len(paths))
	for _, path := range paths {
		v, ok := layers[path]
		if !ok {
			v = settingView{Human: notSetValue, Source: notSetSource}
		}
		v.Path, v.Overrides = path, setBy[path]
		out = append(out, v)
	}
	return out
}

// settingsSection lists every setting on one row, grouped by the path of its
// parent message, as scan_gate.licenses. A value that does not fit the width
// wraps after a list item. The SOURCE column fits the layers and the
// override sources up to a third of the width. A longer override source
// takes a line of its own, so one long override name does not push every
// row past the terminal edge.
func (r *showResult) settingsSection(now time.Time, width int) string {
	settings := settingViews(r.DefaultFields, r.BaseFields, r.Overrides, now)
	parts := []string{style.Heading("Settings")}
	if r.BaseDescription != "" {
		parts = append(parts, wrapText("Base: "+clitui.EscapeLine(r.BaseDescription), width))
	}

	labelW, sourceW := lipgloss.Width("SETTING"), lipgloss.Width("SOURCE")
	for _, s := range settings {
		_, leaf := splitPath(s.Path)
		labelW = max(labelW, lipgloss.Width("  "+leaf))
		sourceW = max(sourceW, lipgloss.Width(s.Source))
	}
	fits := func(source string) bool { return width <= 0 || lipgloss.Width(source) <= width/3 }
	for _, s := range settings {
		for _, o := range s.Overrides {
			if source := clitui.EscapeLine(o.Source); fits(source) {
				sourceW = max(sourceW, lipgloss.Width(source))
			}
		}
	}
	valueW := 0
	if width > 0 {
		valueW = max(width-labelW-sourceW-2*len(columnGap), minWrapWidth)
	}
	row := func(label, source, value string) string {
		return cloudpolicy.PadRight(label, labelW) + columnGap + cloudpolicy.PadRight(source, sourceW) + columnGap + value
	}
	values := func(label, source, value, wrapAt string) []string {
		var out []string
		for _, line := range wrapValue(clitui.EscapeLine(value), wrapAt, valueW) {
			out = append(out, row(label, source, line))
			label, source = "", ""
		}
		return out
	}

	rows := []string{row("SETTING", "SOURCE", "VALUE")}
	group := ""
	hasOverride := false
	for _, s := range settings {
		parent, leaf := splitPath(s.Path)
		if parent != group {
			rows = append(rows, parent)
			group = parent
		}
		wrapAt := listSeparator
		if s.Source == notSetSource {
			wrapAt = " "
		}
		rows = append(rows, values("  "+leaf, s.Source, s.Human, wrapAt)...)
		for _, o := range s.Overrides {
			hasOverride = true
			source := clitui.EscapeLine(o.Source)
			if fits(source) {
				rows = append(rows, values("", source, o.Human, listSeparator)...)
				continue
			}
			for _, line := range wrapValue(source, " ", width-len(wideSourceIndent)) {
				rows = append(rows, wideSourceIndent+line)
			}
			rows = append(rows, values("", "", o.Human, listSeparator)...)
		}
	}
	parts = append(parts, strings.Join(rows, "\n"))

	note := "See the settings document: safedep policy get"
	if hasOverride {
		note = "An override sets its value only for the subjects that its selector matches. " + note
	}
	if r.UnknownSettings {
		parts = append(parts, wrapText("This safedep CLI does not know some settings of this policy. "+
			"Update the CLI to the latest release to see them.", width))
	}
	parts = append(parts, wrapText(note, width))
	return strings.Join(parts, "\n")
}
