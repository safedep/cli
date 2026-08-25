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
// resume where it stopped; the verbs here let an operator move or clear it
// without editing the SQLite state file by hand.
func cursorCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cursor",
		Short: "Manage the saved feed cursor for the active SafeDep profile",
		Long: `Manage the saved feed cursor for the active SafeDep profile.

The 'run' command stores a cursor so it resumes where it stopped. These verbs
let you move it or clear it, the supported alternative to editing the SQLite
state file by hand. The cursor is per profile, so a change affects only the
profile selected with --profile.`,
	}

	cmd.AddCommand(cursorSetCmd(a), cursorRemoveCmd(a))
	return cmd
}

// cursorSetCmd sets the cursor to a given timestamp so the next run processes
// reports updated after it.
func cursorSetCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "set <timestamp>",
		Short: "Set the feed cursor to an RFC3339 timestamp",
		Long: `Set the saved feed cursor to an RFC3339 timestamp.

The next run processes reports updated strictly after this timestamp. Use it to
re-process from a chosen point, for example after a dry-run. Only the profile
selected with --profile is affected.

The timestamp is RFC3339, for example 2026-08-25T10:00:00Z.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ts, err := time.Parse(time.RFC3339, args[0])
			if err != nil {
				return fmt.Errorf("cursor set: %q is not an RFC3339 timestamp (e.g. 2026-08-25T10:00:00Z): %w", args[0], err)
			}

			store, err := openCursorStore(a)
			if err != nil {
				return err
			}
			if err := store.save(cmd.Context(), cursorState{LastSeenAt: ts.UTC()}); err != nil {
				return fmt.Errorf("cursor set: %w", err)
			}

			drytui.Success("Cursor set for profile %q to %s", a.Profile(), ts.UTC().Format(time.RFC3339))
			drytui.Info("The next run processes reports updated after this time")
			return nil
		},
	}
}

// cursorRemoveCmd deletes the saved cursor so the next run starts fresh (or from
// --backfill).
func cursorRemoveCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Remove the saved feed cursor so the next run starts fresh",
		Long: `Remove the saved feed cursor for the active SafeDep profile.

The next run starts fresh from now, or from the window given by --backfill. Only
the profile selected with --profile is affected.

A dry-run ('run --dry-run') advances the same saved cursor. Run this before the
first real run so it re-processes what the preview consumed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openCursorStore(a)
			if err != nil {
				return err
			}

			prev, err := store.load(cmd.Context())
			if err != nil {
				return fmt.Errorf("cursor remove: load cursor: %w", err)
			}
			if prev.LastSeenAt.IsZero() {
				drytui.Info("No saved cursor for profile %q. Nothing to remove.", a.Profile())
				return nil
			}

			if err := store.remove(cmd.Context()); err != nil {
				return fmt.Errorf("cursor remove: %w", err)
			}

			drytui.Success("Cursor removed for profile %q (was %s)", a.Profile(), prev.LastSeenAt.UTC().Format(time.RFC3339))
			drytui.Info("The next run starts fresh from now, or from --backfill")
			if path, err := config.DBPath(); err == nil {
				drytui.Info("Cursor storage: %s", path)
			}
			return nil
		},
	}
}

// openCursorStore opens the profile-scoped cursor store shared by the cursor
// verbs.
func openCursorStore(a *app.App) (*cursorStore, error) {
	kv, err := app.ProfileKV[cursorState](a, kvNamespace)
	if err != nil {
		return nil, fmt.Errorf("cursor: open cursor store: %w", err)
	}
	return newCursorStore(kv), nil
}
