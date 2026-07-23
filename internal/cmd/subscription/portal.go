package subscription

import (
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

func portalCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "portal",
		Short: "Billing portal access",
		Long:  "Access the provider-hosted billing portal for the tenant account.",
	}
	parent.AddCommand(portalOpenCmd(a))
	return parent
}

func portalOpenCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "open",
		Short: "Open the billing portal",
		Long: "Open the provider-hosted billing portal to manage payment methods, invoices, and " +
			"cancellation for the tenant account.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			url, err := NewService(client.Connection()).Portal(cmd.Context(), portalReturnURL)
			if err != nil {
				return err
			}
			openInBrowser(url, "Opening your billing portal...")
			tui.Info("Manage payment methods, invoices, and cancellation there, then return here.")
			return nil
		},
	}
}
