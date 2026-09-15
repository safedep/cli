// Package crowdstrike implements the SafeDep -> CrowdStrike endpoint block event
// integration. It polls SafeDep endpoint package-guard block events (malicious
// package blocks and dependency cooldown blocks) on an interval, resuming from a
// KV-backed cursor, and routes each event to a sink. Stage 1 logs the events; a
// later stage will push them to CrowdStrike SIEM.
package crowdstrike

import (
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// Register attaches the `crowdstrike` sub-command (and its verbs) under the
// supplied parent. Called by the `integration` package during root command
// assembly.
func Register(parent *cobra.Command, a *app.App) {
	cmd := &cobra.Command{
		Use:   "crowdstrike",
		Short: "CrowdStrike endpoint integration commands",
		Long: `Stream SafeDep endpoint package-guard block events for delivery to CrowdStrike.

The integration pulls malicious-package block events and dependency-cooldown
block events from SafeDep endpoint telemetry, incrementally by cursor. Stage 1
logs the events; a later stage will push them to CrowdStrike SIEM.

Authentication uses the active SafeDep profile (see 'safedep auth login') for the
SafeDep API.`,
	}

	cmd.AddCommand(runCmd(a), cursorCmd(a))
	parent.AddCommand(cmd)
}
