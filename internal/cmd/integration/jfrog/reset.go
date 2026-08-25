package jfrog

import (
	"fmt"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	drytui "github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

// cursorCmd groups maintenance of the saved feed cursor. The cursor lets `run`
// resume where it stopped; the verbs here let an operator inspect or clear it
// without editing the SQLite state file by hand.
func cursorCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cursor",
		Short: "Manage the saved feed cursor for the active SafeDep profile",
		Long: `Manage the saved feed cursor for the active SafeDep profile.

The 'run' command stores a cursor so it resumes where it stopped. These verbs
let you clear it, the supported alternative to editing the SQLite state file by
hand. The cursor is per profile, so a change affects only the profile selected
with --profile.`,
	}

	cmd.AddCommand(cursorResetCmd(a))
	return cmd
}

// cursorResetCmd clears the saved cursor so the next `run` starts fresh (or from
// --backfill).
func cursorResetCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Clear the saved feed cursor so the next run starts fresh",
		Long: `Clear the saved feed cursor for the active SafeDep profile.

Reset it to re-process the feed from scratch: the next run starts fresh from
now, or from the window given by --backfill. Only the profile selected with
--profile is affected.

A dry-run ('run --dry-run') advances the same saved cursor. Run this before the
first real run so it re-processes what the preview consumed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kv, err := app.ProfileKV[cursorState](a, kvNamespace)
			if err != nil {
				return fmt.Errorf("cursor reset: open cursor store: %w", err)
			}
			store := newCursorStore(kv)

			prev, err := store.load(cmd.Context())
			if err != nil {
				return fmt.Errorf("cursor reset: load cursor: %w", err)
			}
			if prev.LastSeenAt.IsZero() {
				drytui.Info("No saved cursor for profile %q. Nothing to reset.", a.Profile())
				return nil
			}

			if err := store.reset(cmd.Context()); err != nil {
				return fmt.Errorf("cursor reset: %w", err)
			}

			drytui.Success("Cursor reset for profile %q (was %s)", a.Profile(), prev.LastSeenAt.UTC().Format(time.RFC3339))
			drytui.Info("The next run starts fresh from now, or from --backfill")
			if path, err := config.DBPath(); err == nil {
				drytui.Info("Cursor storage: %s", path)
			}
			return nil
		},
	}
}
