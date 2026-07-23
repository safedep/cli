package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	tuioutput "github.com/safedep/dry/tui/output"
	"github.com/safedep/dry/tui/panel"
	"github.com/spf13/cobra"
)

// customerForm holds the billing-customer flag values shared by the
// customer, trial, and create commands.
type customerForm struct {
	Name    string
	Phone   string
	Country string
	State   string
	City    string
	Postal  string
	Line1   string
	Line2   string
	TaxID   string
}

func addCustomerFlags(cmd *cobra.Command, f *customerForm) {
	fl := cmd.Flags()
	fl.StringVar(&f.Name, "company", "", "company or customer name")
	fl.StringVar(&f.Phone, "phone", "", "contact phone number")
	fl.StringVar(&f.Country, "country", "", "billing country (ISO 3166-1 alpha-2, e.g. US)")
	fl.StringVar(&f.State, "state", "", "billing state/region (ISO 3166-2, e.g. CA)")
	fl.StringVar(&f.City, "city", "", "billing city")
	fl.StringVar(&f.Postal, "postal", "", "billing postal code")
	fl.StringVar(&f.Line1, "line1", "", "billing address line 1")
	fl.StringVar(&f.Line2, "line2", "", "billing address line 2 (optional)")
	fl.StringVar(&f.TaxID, "tax-id", "", "tax ID (optional)")
}

// interactive reports whether we can prompt the user. Rich mode implies a
// human at a TTY; agent/plain/CI modes must supply values via flags.
func interactive() bool {
	return tuioutput.CurrentMode() == tuioutput.Rich
}

// resolveCustomerInput turns the form into a CustomerInput, prompting for
// any missing required field when interactive, or erroring with the missing
// flags otherwise.
func resolveCustomerInput(f customerForm) (CustomerInput, error) {
	in := CustomerInput(f)
	if interactive() {
		if err := promptCustomer(&in); err != nil {
			return CustomerInput{}, err
		}
	}

	var missing []string
	for _, req := range []struct {
		flag string
		val  string
	}{
		{"--company", in.Name}, {"--phone", in.Phone}, {"--country", in.Country},
		{"--state", in.State}, {"--city", in.City}, {"--postal", in.Postal}, {"--line1", in.Line1},
	} {
		if strings.TrimSpace(req.val) == "" {
			missing = append(missing, req.flag)
		}
	}
	if len(missing) > 0 {
		return CustomerInput{}, fmt.Errorf("missing billing details: %s (or run interactively in a terminal)", strings.Join(missing, ", "))
	}
	return in, nil
}

func promptCustomer(in *CustomerInput) error {
	fields := []struct {
		title    string
		desc     string
		val      *string
		required bool
	}{
		{"Company / name", "", &in.Name, true},
		{"Phone", "", &in.Phone, true},
		{"Country", "ISO 3166-1 alpha-2, e.g. US", &in.Country, true},
		{"State / region", "ISO 3166-2, e.g. CA", &in.State, true},
		{"City", "", &in.City, true},
		{"Postal code", "", &in.Postal, true},
		{"Address line 1", "", &in.Line1, true},
		{"Address line 2", "optional", &in.Line2, false},
		{"Tax ID", "optional", &in.TaxID, false},
	}
	for _, f := range fields {
		if strings.TrimSpace(*f.val) != "" {
			continue // already supplied via flag
		}
		input := huh.NewInput().Title(f.title).Value(f.val)
		if f.desc != "" {
			input = input.Description(f.desc)
		}
		if err := input.Run(); err != nil {
			return fmt.Errorf("billing details prompt: %w", err)
		}
		*f.val = strings.TrimSpace(*f.val)
		if f.required && *f.val == "" {
			return fmt.Errorf("%s is required", f.title)
		}
	}
	return nil
}

// ensureCustomer guarantees a billing customer exists, creating one from the
// form (interactive or flags) when absent. Shared by trial and create.
func ensureCustomer(ctx context.Context, svc customerSvc, f customerForm) error {
	_, exists, err := svc.GetCustomer(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if interactive() {
		tui.Info("No billing profile yet - let's set one up (required to continue).")
	}
	in, err := resolveCustomerInput(f)
	if err != nil {
		return err
	}
	_, perr, err := svc.CreateCustomer(ctx, in)
	if err != nil {
		return err
	}
	if len(perr) > 0 {
		return fmt.Errorf("billing provider rejected the customer: %s", perr[0].Message)
	}
	tui.Success("Billing profile created.")
	return nil
}

type customerSvc interface {
	CustomerGetter
	CustomerCreator
}

func customerCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "customer",
		Short: "Manage the billing customer profile",
		Long:  "Create and inspect the billing customer profile for the tenant account.",
	}
	parent.AddCommand(createCustomerCmd(a))
	parent.AddCommand(showCustomerCmd(a))
	return parent
}

func createCustomerCmd(a *app.App) *cobra.Command {
	var form customerForm
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create the billing customer profile",
		Long:  "Create the billing customer profile for the tenant account. Prompts interactively on a terminal; requires flags otherwise.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			svc := NewService(client.Connection())
			if _, exists, err := svc.GetCustomer(cmd.Context()); err != nil {
				return err
			} else if exists {
				return errors.New("a billing customer already exists for this account")
			}
			in, err := resolveCustomerInput(form)
			if err != nil {
				return err
			}
			cust, perr, err := svc.CreateCustomer(cmd.Context(), in)
			if err != nil {
				return err
			}
			if len(perr) > 0 {
				return fmt.Errorf("billing provider rejected the customer: %s", perr[0].Message)
			}
			return a.Output.Print(&customerResult{cust: cust})
		},
	}
	addCustomerFlags(cmd, &form)
	return cmd
}

func showCustomerCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the billing customer profile",
		Long:  "Show the billing customer profile linked to the tenant account.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			cust, exists, err := NewService(client.Connection()).GetCustomer(cmd.Context())
			if err != nil {
				return err
			}
			if !exists {
				return errors.New("no billing customer yet: create one with `safedep subscription customer create`")
			}
			return a.Output.Print(&customerResult{cust: cust})
		},
	}
}

type customerResult struct{ cust *Customer }

func (r *customerResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(map[string]any{
		"id": r.cust.ID, "name": r.cust.Name, "email": r.cust.Email, "phone": r.cust.Phone,
		"country": r.cust.Country, "state": r.cust.State, "city": r.cust.City,
		"postal_code": r.cust.Postal, "line1": r.cust.Line1, "line2": r.cust.Line2, "tax_id": r.cust.TaxID,
	}, "", "  ")
}

func (r *customerResult) RenderPlain() string {
	return strings.Join([]string{
		r.cust.Name, r.cust.Email, r.cust.Phone,
		fmt.Sprintf("%s, %s %s, %s", r.cust.City, r.cust.State, r.cust.Postal, r.cust.Country),
	}, "\t")
}

func (r *customerResult) RenderTable() string {
	return panel.New("Billing customer").
		Field("Name", r.cust.Name).
		Field("Email", r.cust.Email).
		Field("Phone", r.cust.Phone).
		Field("Address", fmt.Sprintf("%s, %s, %s %s, %s", r.cust.Line1, r.cust.City, r.cust.State, r.cust.Postal, r.cust.Country)).
		FieldIf(r.cust.TaxID != "", "Tax ID", r.cust.TaxID).
		Render()
}
