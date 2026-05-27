// Package endpoint registers the `safedep endpoint` command tree, the
// CLI front-end for SafeDep Cloud's EndpointManagementService (Endpoint
// Hub). All verbs require control-plane credentials.
package endpoint

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "endpoint",
		Short: "Inspect endpoints reporting to SafeDep Cloud",
		Long:  "View fleet health, inspect individual endpoints, audit recent activity, and inspect inventory across endpoints reporting to SafeDep Cloud (Endpoint Hub).",
	}
	parent.AddCommand(
		statusCmd(a),
		listCmd(a),
		showCmd(a),
		activityCmd(a),
		inventoryCmd(a),
	)
	root.AddCommand(parent)
}

func activityCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "activity",
		Short: "Audit recent endpoint activity",
		Long:  "Audit recent activity across endpoints, including blocked package installs and newly detected inventory items.",
	}
	parent.AddCommand(activityListCmd(a))
	return parent
}

func inventoryCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "inventory",
		Short: "Inspect endpoint inventory",
		Long:  "Inspect the current inventory snapshot reported by endpoints (deduped from raw inventory events).",
	}
	parent.AddCommand(inventoryListCmd(a))
	return parent
}
