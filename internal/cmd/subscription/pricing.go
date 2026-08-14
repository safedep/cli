package subscription

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/spf13/cobra"
)

func pricingCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "pricing",
		Short: "Show published prices",
		Long: "Show the published list prices for plans and add-ons. Prices are the same for every " +
			"tenant and do not include a tenant discount or the exact amount charged at purchase.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runPricing(cmd.Context(), NewService(client.Connection()))
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
}

func runPricing(ctx context.Context, svc CatalogGetter) (*pricingResult, error) {
	cat, err := svc.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	return &pricingResult{catalog: cat}, nil
}

type pricingResult struct{ catalog *Catalog }

// priceLabel is the cadence label for a price row. A metered price reads
// "Per <unit>" (for example "Per scan"), or "Per unit" when the product has no
// unit.
func priceLabel(prod CatalogProduct, p CatalogPrice) string {
	if p.Metered {
		if prod.PricingUnit != "" {
			return "Per " + prod.PricingUnit
		}
		return "Per unit"
	}
	switch p.Interval {
	case "monthly":
		return "Monthly"
	case "yearly":
		return "Yearly"
	default:
		return "Price"
	}
}

// priceValue is the amount for a price row. A licensed price on a per-unit
// product reads "$20.00 per endpoint"; the metered unit is already in the label.
func priceValue(prod CatalogProduct, p CatalogPrice) string {
	amount := formatMoney(p.UnitAmountMinor, p.Currency)
	if !p.Metered && prod.PricingUnit != "" {
		amount += " per " + prod.PricingUnit
	}
	return amount
}

func (r *pricingResult) RenderJSON() ([]byte, error) {
	type price struct {
		Interval        string `json:"interval"`
		Metered         bool   `json:"metered"`
		Currency        string `json:"currency"`
		UnitAmountMinor int64  `json:"unit_amount_minor"`
	}
	type product struct {
		Name        string  `json:"name"`
		Kind        string  `json:"kind"`
		AddOn       string  `json:"add_on,omitempty"`
		PricingUnit string  `json:"pricing_unit,omitempty"`
		Prices      []price `json:"prices"`
	}
	products := make([]product, 0, len(r.catalog.Products))
	for _, p := range r.catalog.Products {
		prices := make([]price, 0, len(p.Prices))
		for _, pr := range p.Prices {
			prices = append(prices, price{
				Interval:        pr.Interval,
				Metered:         pr.Metered,
				Currency:        pr.Currency,
				UnitAmountMinor: pr.UnitAmountMinor,
			})
		}
		products = append(products, product{
			Name: p.DisplayName, Kind: p.Kind, AddOn: p.AddOn, PricingUnit: p.PricingUnit, Prices: prices})
	}
	return json.MarshalIndent(map[string]any{"products": products}, "", "  ")
}

func (r *pricingResult) RenderPlain() string {
	if len(r.catalog.Products) == 0 {
		return "pricing\t-"
	}
	var lines []string
	for _, p := range r.catalog.Products {
		for _, pr := range p.Prices {
			lines = append(lines, strings.Join(
				[]string{p.DisplayName, priceLabel(p, pr), priceValue(p, pr)}, "\t"))
		}
	}
	return strings.Join(lines, "\n")
}

func (r *pricingResult) RenderTable() string {
	if len(r.catalog.Products) == 0 {
		return panel.New("Pricing").Field("Products", "-").Render()
	}
	panels := make([]string, 0, len(r.catalog.Products)+1)
	for _, p := range r.catalog.Products {
		pn := panel.New(p.DisplayName)
		for _, pr := range p.Prices {
			pn = pn.Field(priceLabel(p, pr), priceValue(p, pr))
		}
		panels = append(panels, pn.Render())
	}
	panels = append(panels,
		section.Hint("List prices, the same for every tenant. The amount charged may differ with a discount."))
	return section.Join(panels...)
}
