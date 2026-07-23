// Package subscription registers the `safedep subscription` domain: the
// tenant's plan and access lifecycle - status, free trial, paid subscription,
// on-demand (overage) billing, and the billing portal. It is named for the
// user's intent (plan/access), not the billing mechanism; low-level billing
// detail (invoices, payment methods) is delegated to the provider portal.
package subscription

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register wires the subscription command tree onto root.
func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "subscription",
		Short: "Manage your SafeDep subscription",
		Long:  "Manage the tenant account's plan: status, free trial, subscription, on-demand billing, and the billing portal.",
	}

	parent.AddCommand(statusCmd(a))
	parent.AddCommand(trialCmd(a))
	parent.AddCommand(createCmd(a))
	parent.AddCommand(ondemandCmd(a))
	parent.AddCommand(customerCmd(a))
	parent.AddCommand(portalCmd(a))

	root.AddCommand(parent)
}
