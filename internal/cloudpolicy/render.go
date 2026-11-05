package cloudpolicy

import (
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/tui"
)

// Separator joins the parts of a summary line, as in "revision 47",
// separator, "published 6m ago by alice". It is a middle dot with spaces.
const Separator = " · "

// KeyValues writes label and value rows with the values in one column. The
// column uses the display width of the labels. Each value is escaped as one
// line, because a value can come from the server. A value that needs more
// lines takes more rows: a row with an empty label continues the row above.
func KeyValues(rows [][2]string) string {
	width := 0
	for _, r := range rows {
		width = max(width, lipgloss.Width(r[0]))
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(r[0])
		b.WriteString(strings.Repeat(" ", width-lipgloss.Width(r[0])+2))
		b.WriteString(tui.EscapeLine(r[1]))
	}
	return b.String()
}

// EmptyDash keeps a table cell readable when a value is absent.
func EmptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Plural picks the singular or the plural form for n.
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// ActorName names an actor for people: the display name, or the id.
func ActorName(a *messagescontroltowerv1.Actor) string {
	if a.GetDisplayName() != "" {
		return a.GetDisplayName()
	}
	return a.GetId()
}

// TimeOf returns a timestamp in UTC, or nil when it is absent.
func TimeOf(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime().UTC()
	return &t
}
