// Package audit registers the `safedep audit` command tree. It reads the
// audit events of the tenant from SafeDep Cloud: who did an admin action,
// what, to which resource, when, and with which result. Every verb needs
// control plane credentials and the Admin role.
package audit

import (
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

func Register(root *cobra.Command, a *app.App) {
	parent := &cobra.Command{
		Use:   "audit",
		Short: "Read the audit log of your SafeDep Cloud tenant",
		Long: "Read the audit log of your SafeDep Cloud tenant. Each admin action, such as a policy " +
			"publish or a label change, writes one audit event. Reading the audit log needs the Admin role.",
	}
	event := &cobra.Command{
		Use:   "event",
		Short: "List audit events",
		Long:  "List the audit events of the tenant, newest first, with filters for time, action, resource and actor.",
	}
	event.AddCommand(eventListCmd(a))
	parent.AddCommand(event)
	root.AddCommand(parent)
}
