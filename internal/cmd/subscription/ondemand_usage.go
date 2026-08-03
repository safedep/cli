package subscription

import (
	"fmt"
	"strings"

	"github.com/safedep/dry/tui/meter"
	"github.com/safedep/dry/tui/section"
)

// renderFeatureUsage renders one feature's context line, its bar(s), and an
// optional next-step hint. Presentation only: it branches on the numbers the
// server already computed and never derives billing values.
func renderFeatureUsage(fu FeatureUsage, enabled bool) string {
	lines := []string{usageContextLine(fu)}

	switch {
	case fu.IncludedLimit == 0: // not available on this tier
		lines = append(lines, section.Hint("Not available on your plan"))
	case fu.IncludedLimit < 0: // unlimited
		lines = append(lines, "Included   Unlimited")
	default:
		bars := []meter.Bar{includedBar(fu)}
		if ob, ok := overageBar(fu, enabled); ok {
			bars = append(bars, ob)
		}
		lines = append(lines, meter.Render(bars...))
	}

	if hint := usageHint(fu, enabled); hint != "" {
		lines = append(lines, section.Hint(hint))
	}
	return strings.Join(lines, "\n")
}

func usageContextLine(fu FeatureUsage) string {
	period := "this month"
	if !fu.PeriodEnd.IsZero() {
		period = fmt.Sprintf("this month (resets %s)", fu.PeriodEnd.Format("2 Jan"))
	}

	seats := fmt.Sprintf("%d seat", fu.Seats)
	if fu.Seats != 1 {
		seats += "s"
	}

	var allowance string
	switch {
	case fu.IncludedLimit < 0:
		allowance = "unlimited"
	case fu.IncludedLimit == 0:
		allowance = "not available"
	default:
		allowance = fmt.Sprintf("%d %s/mo", fu.IncludedLimit, fu.UnitLabel)
	}

	line := fmt.Sprintf("%s: %s, %s, %s", fu.DisplayName, period, allowance, seats)
	if fu.Provisional {
		line += " (preview: not enforced)"
	}
	return line
}

func includedBar(fu FeatureUsage) meter.Bar {
	reading := "full"
	if fu.Consumed < fu.IncludedLimit {
		reading = fmt.Sprintf("%d%%", percentInt(fu.Consumed, fu.IncludedLimit))
	}
	label := "Included"
	if fu.Provisional {
		label = "Used"
	}
	return meter.Bar{
		Label:     label,
		Value:     fu.Consumed,
		Max:       fu.IncludedLimit,
		ValueText: fmt.Sprintf("%d / %d %s (%s)", fu.Consumed, fu.IncludedLimit, fu.UnitLabel, reading),
		Warn:      !fu.Provisional && fu.Consumed >= fu.IncludedLimit,
	}
}

// overageBar is shown when there is accrued overage to display, or when
// on-demand is enabled and the included allowance is exhausted. Keying on
// OverageUsed (not Enabled alone) keeps an accrued charge visible while it is
// still settling after on-demand is disabled.
func overageBar(fu FeatureUsage, enabled bool) (meter.Bar, bool) {
	ov := fu.Overage
	if ov == nil {
		return meter.Bar{}, false
	}
	show := fu.OverageUsed > 0 || (enabled && fu.Consumed >= fu.IncludedLimit)
	if !show {
		return meter.Bar{}, false
	}

	if ov.CapKind == "monetary" {
		text := fmt.Sprintf("%s of %s cap", money(fu.OverageUsedMinor, ov.Currency), money(ov.CapAmountMinor, ov.Currency))
		if fu.SettlementPending {
			text += " (settling)"
		}
		return meter.Bar{
			Label:     "Overage",
			Value:     fu.OverageUsedMinor,
			Max:       ov.CapAmountMinor,
			ValueText: text,
			Warn:      capReached(fu),
		}, true
	}

	text := fmt.Sprintf("%d / %d %s", fu.OverageUsed, ov.CapUnits, fu.UnitLabel)
	if fu.SettlementPending {
		text += " (settling)"
	}
	return meter.Bar{
		Label:     "Overage",
		Value:     fu.OverageUsed,
		Max:       ov.CapUnits,
		ValueText: text,
		Warn:      capReached(fu),
	}, true
}

func usageHint(fu FeatureUsage, enabled bool) string {
	if fu.Overage == nil {
		return ""
	}
	switch {
	case fu.SettlementPending:
		return "Accrued overage is still being billed"
	case capReached(fu):
		return "Cap reached: contact SafeDep to raise your cap"
	case !enabled && fu.IncludedLimit > 0 && fu.Consumed >= fu.IncludedLimit:
		return "Enable on-demand to continue: safedep subscription ondemand enable"
	default:
		return ""
	}
}

// plainUsage is the Plain/Agent rendering of the included reading, spelling out
// the unlimited and not-available sentinels rather than printing -1 or 0.
func plainUsage(fu FeatureUsage) string {
	switch {
	case fu.IncludedLimit < 0:
		return fmt.Sprintf("%d/unlimited", fu.Consumed)
	case fu.IncludedLimit == 0:
		return fmt.Sprintf("%d/unavailable", fu.Consumed)
	default:
		return fmt.Sprintf("%d/%d", fu.Consumed, fu.IncludedLimit)
	}
}

// plainOverage renders the accrued overage in its natural unit: money for a
// monetary cap, unit count otherwise.
func plainOverage(fu FeatureUsage) string {
	if fu.Overage != nil && fu.Overage.CapKind == "monetary" {
		return money(fu.OverageUsedMinor, fu.Overage.Currency)
	}
	return fmt.Sprintf("%d", fu.OverageUsed)
}

// capReached reports whether accrued overage has hit the feature's cap, in the
// cap's own unit (monetary amount or unit count).
func capReached(fu FeatureUsage) bool {
	ov := fu.Overage
	if ov == nil {
		return false
	}
	if ov.CapKind == "monetary" {
		return ov.CapAmountMinor > 0 && fu.OverageUsedMinor >= ov.CapAmountMinor
	}
	return ov.CapUnits > 0 && fu.OverageUsed >= ov.CapUnits
}

// money formats minor currency units (e.g. cents) as a display amount.
func money(minor int64, currency string) string {
	symbol := "$"
	if c := strings.ToLower(currency); c != "" && c != "usd" {
		symbol = strings.ToUpper(currency) + " "
	}
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%s%d.%02d", sign, symbol, minor/100, minor%100)
}

func percentInt(value, max int64) int {
	if max <= 0 || value <= 0 {
		return 0
	}
	if value >= max {
		return 100
	}
	return int((value * 100) / max)
}
