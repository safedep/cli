package subscription

import (
	"fmt"
	"strings"

	"github.com/safedep/dry/tui/meter"
	"github.com/safedep/dry/tui/table"
)

// renderEndpointUsage renders the SDLC Endpoint units bar and the per-class
// breakdown. The breakdown is mandatory rendering data: the rows explain the
// units total next to the endpoint and project pages. Presentation only: the
// server computed every number.
func renderEndpointUsage(eu *EndpointUsage) string {
	lines := []string{endpointContextLine(eu)}

	if eu.HasIncluded {
		reading := "full"
		if eu.UnitsUsed < eu.UnitsIncluded {
			reading = fmt.Sprintf("%d%%", percentInt(eu.UnitsUsed, eu.UnitsIncluded))
		}
		lines = append(lines, meter.Render(meter.Bar{
			Label:     "Units",
			Value:     eu.UnitsUsed,
			Max:       eu.UnitsIncluded,
			ValueText: fmt.Sprintf("%d / %d units (%s)", eu.UnitsUsed, eu.UnitsIncluded, reading),
			Warn:      eu.UnitsUsed >= eu.UnitsIncluded,
		}))
	} else {
		lines = append(lines, fmt.Sprintf("Units used   %d (account has no defined allotment)", eu.UnitsUsed))
	}

	if len(eu.Breakdown) > 0 {
		rows := make([][]string, 0, len(eu.Breakdown))
		for _, row := range eu.Breakdown {
			rows = append(rows, []string{
				row.DisplayName,
				fmt.Sprintf("%d", row.ActiveAssets),
				fmt.Sprintf("%d", row.AssetsPerUnit),
				fmt.Sprintf("%d", row.Units),
			})
		}
		lines = append(lines, table.New().Headers("Class", "Active", "Assets/unit", "Units").Rows(rows...).Render())
	}

	return strings.Join(lines, "\n")
}

func endpointContextLine(eu *EndpointUsage) string {
	period := "this month"
	if !eu.PeriodEnd.IsZero() {
		period = fmt.Sprintf("this month (resets %s)", eu.PeriodEnd.Format("2 Jan"))
	}
	return "SDLC Endpoints: " + period
}
