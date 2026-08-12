package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCatalog() *Catalog {
	return &Catalog{Products: []CatalogProduct{
		{
			DisplayName: "SafeDep Cloud",
			Kind:        "subscription_tier",
			PricingUnit: "seat",
			Prices: []CatalogPrice{
				{UnitAmountMinor: 2000, Currency: "usd", Interval: "monthly"},
				{UnitAmountMinor: 19200, Currency: "usd", Interval: "yearly"},
			},
		},
		{
			DisplayName: "SafeDep Threat Intel",
			Kind:        "add_on",
			AddOn:       threatIntelAddOn,
			Prices: []CatalogPrice{
				{UnitAmountMinor: 199900, Currency: "usd", Interval: "monthly"},
				{UnitAmountMinor: 2398800, Currency: "usd", Interval: "yearly"},
			},
		},
		{
			DisplayName: "On-demand Package Scan",
			Kind:        "overage",
			PricingUnit: "scan",
			Prices: []CatalogPrice{
				{UnitAmountMinor: 50, Currency: "usd", Interval: "monthly", Metered: true},
			},
		},
	}}
}

func TestFormatMoney(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "$19.99", formatMoney(1999, "usd"))
	assert.Equal(t, "$1999.00", formatMoney(199900, "usd"))
	assert.Equal(t, "$0.50", formatMoney(50, "usd"))
	assert.Equal(t, "-$1.00", formatMoney(-100, "usd"))
	assert.Equal(t, "10.00 EUR", formatMoney(1000, "eur"))
}

func TestRunPricing_RendersProductsAndPrices(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{catalogFn: func(context.Context) (*Catalog, error) { return testCatalog(), nil }}

	res, err := runPricing(context.Background(), svc)
	require.NoError(t, err)

	table := res.RenderTable()
	assert.Contains(t, table, "SafeDep Threat Intel")
	assert.Contains(t, table, "$1999.00")
	assert.Contains(t, table, "Per scan") // metered overage names its unit
	assert.Contains(t, table, "per seat") // seat-priced tier

	plain := res.RenderPlain()
	assert.Contains(t, plain, "SafeDep Cloud\tMonthly\t$20.00 per seat")

	js, err := res.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js), "\"unit_amount_minor\": 199900")
	assert.Contains(t, string(js), "\"add_on\": \"threat-intel-feed\"")
	assert.Contains(t, string(js), "\"pricing_unit\": \"seat\"")
}

func TestRunPricing_PropagatesError(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{catalogFn: func(context.Context) (*Catalog, error) { return nil, errors.New("boom") }}
	_, err := runPricing(context.Background(), svc)
	require.Error(t, err)
}

func TestAddOnPriceLabel(t *testing.T) {
	t.Parallel()

	svc := &fakeSvc{catalogFn: func(context.Context) (*Catalog, error) { return testCatalog(), nil }}
	assert.Equal(t, "$1999.00/mo, $23988.00/yr", addOnPriceLabel(context.Background(), svc, threatIntelAddOn))
	assert.Empty(t, addOnPriceLabel(context.Background(), svc, "unknown-add-on"))

	// A catalog fetch failure must not block the confirm.
	failing := &fakeSvc{catalogFn: func(context.Context) (*Catalog, error) { return nil, errors.New("down") }}
	assert.Empty(t, addOnPriceLabel(context.Background(), failing, threatIntelAddOn))
}
