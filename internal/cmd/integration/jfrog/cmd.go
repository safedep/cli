package jfrog

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register attaches the `jfrog` sub-command (and its verbs) under the
// supplied parent. Called by the `integration` package during root command
// assembly; not invoked directly by main.
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "jfrog",
		Short: "JFrog XRay integration commands",
		Long: `Integrate SafeDep malicious package threat intelligence with JFrog XRay.

The integration polls the SafeDep Threat Intelligence API for verified malicious
packages and pushes each finding to JFrog XRay as a Custom Issue. Once an issue
is recorded, any XRay security policy with a malware-block action automatically
prevents those packages from being downloaded across the JFrog instance.

Authentication uses the active SafeDep profile (see 'safedep auth login') for
the SafeDep API and a JFrog access token with Manage Xray Metadata permission
for the JFrog side.`,
	}

	cmd.AddCommand(runCmd(a))
	parent.AddCommand(cmd)
}
