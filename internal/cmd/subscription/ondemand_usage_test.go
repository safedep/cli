package subscription

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoney(t *testing.T) {
	assert.Equal(t, "$0.00", money(0, "usd"))
	assert.Equal(t, "$21.50", money(2150, "usd"))
	assert.Equal(t, "$50.00", money(5000, "usd"))
	assert.Equal(t, "-$1.05", money(-105, "usd"))
	assert.Equal(t, "EUR 3.00", money(300, "eur"))
}

func TestPercentInt(t *testing.T) {
	assert.Equal(t, 0, percentInt(0, 100))
	assert.Equal(t, 72, percentInt(72, 100))
	assert.Equal(t, 100, percentInt(100, 100))
	assert.Equal(t, 100, percentInt(150, 100))
	assert.Equal(t, 0, percentInt(10, 0))
}

func TestUsageContextLine(t *testing.T) {
	finite := usageContextLine(FeatureUsage{DisplayName: "On-demand package scans", UnitLabel: "scans", IncludedLimit: 100, Seats: 1})
	assert.Contains(t, finite, "On-demand package scans")
	assert.Contains(t, finite, "100 scans/mo")
	assert.Contains(t, finite, "1 seat")
	assert.NotContains(t, finite, "1 seats")

	multi := usageContextLine(FeatureUsage{DisplayName: "Scans", UnitLabel: "scans", IncludedLimit: 500, Seats: 5})
	assert.Contains(t, multi, "500 scans/mo")
	assert.Contains(t, multi, "5 seats")

	provisional := usageContextLine(FeatureUsage{DisplayName: "Project scans", UnitLabel: "scans", IncludedLimit: 25, Seats: 1, Provisional: true})
	assert.Contains(t, provisional, "preview: not enforced")

	assert.Contains(t, usageContextLine(FeatureUsage{DisplayName: "X", IncludedLimit: -1, Seats: 1}), "unlimited")
	assert.Contains(t, usageContextLine(FeatureUsage{DisplayName: "X", IncludedLimit: 0, Seats: 1}), "not available")
}

func monetaryOverage() *FeatureOverage {
	return &FeatureOverage{CapKind: "monetary", CapUnits: 100, UnitPriceMinor: 50, CapAmountMinor: 5000, Currency: "usd"}
}

func TestOverageBar(t *testing.T) {
	// Accrued overage shows as a money bar.
	bar, ok := overageBar(FeatureUsage{IncludedLimit: 100, Consumed: 143, OverageUsed: 43, OverageUsedMinor: 2150, Overage: monetaryOverage()}, true)
	require.True(t, ok)
	assert.Equal(t, "Overage", bar.Label)
	assert.Equal(t, int64(2150), bar.Value)
	assert.Equal(t, int64(5000), bar.Max)
	assert.Contains(t, bar.ValueText, "$21.50 of $50.00 cap")

	// No overage clause => no bar.
	_, ok = overageBar(FeatureUsage{IncludedLimit: 100, Consumed: 50}, true)
	assert.False(t, ok)

	// Disabled and nothing accrued => no bar.
	_, ok = overageBar(FeatureUsage{IncludedLimit: 100, Consumed: 50, OverageUsed: 0, Overage: monetaryOverage()}, false)
	assert.False(t, ok)

	// Accrued overage stays visible while settling even when disabled.
	bar, ok = overageBar(FeatureUsage{IncludedLimit: 100, Consumed: 120, OverageUsed: 20, OverageUsedMinor: 1000, Overage: monetaryOverage(), SettlementPending: true}, false)
	require.True(t, ok)
	assert.Contains(t, bar.ValueText, "settling")
}

func TestUsageHint(t *testing.T) {
	assert.Empty(t, usageHint(FeatureUsage{IncludedLimit: 25, Consumed: 3}, false), "no overage clause, no hint")

	settling := usageHint(FeatureUsage{IncludedLimit: 100, Consumed: 120, OverageUsed: 20, OverageUsedMinor: 1000, Overage: monetaryOverage(), SettlementPending: true}, false)
	assert.Contains(t, settling, "still being billed")

	capped := usageHint(FeatureUsage{IncludedLimit: 100, Consumed: 200, OverageUsed: 100, OverageUsedMinor: 5000, Overage: monetaryOverage()}, true)
	assert.Contains(t, capped, "contact SafeDep to raise your cap")

	upsell := usageHint(FeatureUsage{IncludedLimit: 100, Consumed: 100, Overage: monetaryOverage()}, false)
	assert.Contains(t, upsell, "ondemand enable")
}

func TestRenderJSONIncludesUsage(t *testing.T) {
	r := &ondemandResult{state: &OnDemandState{
		Enabled: true, PaymentMethodOnFile: true, Posture: "ok",
		Usage: []FeatureUsage{{
			FeatureKey: "malysis.package_scan_submit", DisplayName: "On-demand package scans",
			UnitLabel: "scans", IncludedLimit: 100, Consumed: 72, Seats: 1, Tier: "professional",
			Overage: monetaryOverage(),
		}},
	}}
	b, err := r.RenderJSON()
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	usage, ok := m["feature_usage"].([]any)
	require.True(t, ok)
	require.Len(t, usage, 1)
	entry := usage[0].(map[string]any)
	assert.Equal(t, "malysis.package_scan_submit", entry["feature_key"])
	assert.EqualValues(t, 72, entry["consumed"])
	assert.NotNil(t, entry["overage"])
}

func TestRenderPlainIncludesUsage(t *testing.T) {
	r := &ondemandResult{state: &OnDemandState{
		Enabled: true, Posture: "ok",
		Usage: []FeatureUsage{{FeatureKey: "malysis.package_scan_submit", Consumed: 72, IncludedLimit: 100, OverageUsed: 0}},
	}}
	out := r.RenderPlain()
	assert.Contains(t, out, "usage.malysis.package_scan_submit\t72/100")
	assert.True(t, strings.HasPrefix(out, "enabled\t"))
}
