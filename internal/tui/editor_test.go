package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunEditor_NeedsACommand(t *testing.T) {
	err := RunEditor(context.Background(), "  ", "/tmp/x.yaml")
	assert.ErrorContains(t, err, "no editor is set")
}

func TestRunEditor_ReportsAFailedEditor(t *testing.T) {
	err := RunEditor(context.Background(), "false --flag", "/tmp/x.yaml")
	assert.ErrorContains(t, err, `editor "false"`)
}
