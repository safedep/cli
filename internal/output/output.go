// Package output provides the CLI's structured-output dispatcher.
//
// Presentation (rich/plain/agent) is delegated to dry/tui. JSON output is
// handled here so commands can declare a single Renderer type that works
// across all four user-facing modes.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/safedep/dry/tui"
	tuioutput "github.com/safedep/dry/tui/output"
)

// Mode is the user-facing output selection.
type Mode string

const (
	ModeRich  Mode = "rich"
	ModePlain Mode = "plain"
	ModeAgent Mode = "agent"
	ModeJSON  Mode = "json"
)

// Renderer is implemented by commands that produce structured results.
// Operational commands (login, install) bypass Renderer and call tui.Info /
// tui.Success / tui.Warning / tui.Error directly.
type Renderer interface {
	tui.Renderable
	AsJSON() (any, error)
}

// Output dispatches a Renderer to stdout based on the active mode.
type Output struct {
	mode   Mode
	stdout io.Writer
}

func New(mode Mode) *Output {
	return &Output{mode: mode, stdout: tuioutput.Stdout()}
}

func (o *Output) Mode() Mode { return o.mode }

// Print routes the Renderer based on Mode. JSON encodes AsJSON() to stdout.
// Every other mode delegates to dry/tui.Print which honours its own Mode.
func (o *Output) Print(r Renderer) error {
	if o.mode == ModeJSON {
		v, err := r.AsJSON()
		if err != nil {
			return fmt.Errorf("output: AsJSON: %w", err)
		}
		enc := json.NewEncoder(o.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	tui.Print(r)
	return nil
}

// ParseMode validates a user-supplied --output value. Empty input means
// "auto-detect": we resolve to the dry/tui-detected mode for non-JSON
// presentation. JSON is never auto-selected.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeRich, ModePlain, ModeAgent, ModeJSON:
		return Mode(s), nil
	case "":
		return autoDetect(), nil
	default:
		return "", fmt.Errorf("unknown --output %q: must be rich, plain, agent, or json", s)
	}
}

func autoDetect() Mode {
	switch tuioutput.CurrentMode() {
	case tuioutput.Plain:
		return ModePlain
	case tuioutput.Agent:
		return ModeAgent
	default:
		return ModeRich
	}
}

// AutoMode returns the auto-detected presentation Mode. JSON is never
// auto-selected. It must be requested explicitly via --output.
func AutoMode() Mode { return autoDetect() }

// ApplyToTUI mirrors the resolved Mode into dry/tui's global state so
// tui.Info / tui.Print pick up the same presentation. JSON mode keeps
// dry/tui in its detected presentation mode (used for any progress
// messages routed via stderr) but Print never reaches tui.Print.
func (o *Output) ApplyToTUI() {
	switch o.mode {
	case ModePlain:
		tuioutput.SetMode(tuioutput.Plain)
	case ModeAgent:
		tuioutput.SetMode(tuioutput.Agent)
	case ModeRich:
		tuioutput.SetMode(tuioutput.Rich)
	case ModeJSON:
		// Leave dry/tui auto-detected. Structured output bypasses tui.Print.
	}
}
