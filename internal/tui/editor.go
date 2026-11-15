package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunEditor opens path in the editor command and waits for it to exit. The
// command can carry arguments, as in "code --wait". The editor gets the
// terminal of the CLI, so it lives here with the other terminal code.
func RunEditor(ctx context.Context, command, path string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return errors.New("no editor is set: set VISUAL or EDITOR, as in EDITOR=vi")
	}
	args := append(append([]string{}, parts[1:]...), path)
	//nolint:gosec // G204: the user picks the editor through VISUAL or EDITOR.
	cmd := exec.CommandContext(ctx, parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %w", parts[0], err)
	}
	return nil
}
