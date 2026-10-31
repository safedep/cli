package cloudpolicy

import (
	"strings"
)

// Separator joins the parts of a summary line, as in "revision 47",
// separator, "published 6m ago by alice". It is a middle dot with spaces.
const Separator = " · "

// KeyValues writes label and value rows with the values in one column. A
// value with line breaks keeps the column on each line.
func KeyValues(rows [][2]string) string {
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		lines := strings.Split(r[1], "\n")
		b.WriteString(r[0])
		b.WriteString(strings.Repeat(" ", width-len(r[0])+2))
		b.WriteString(lines[0])
		for _, line := range lines[1:] {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", width+2))
			b.WriteString(line)
		}
	}
	return b.String()
}
