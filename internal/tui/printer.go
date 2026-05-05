package tui

import (
	"fmt"
	"io"

	tuioutput "github.com/safedep/dry/tui/output"
)

// Mode is the user-facing data-output selection. It governs only the
// data stream on stdout. Messaging on stderr (tui.Info/Success/...) is
// driven independently by dry/tui's own mode auto-detection.
type Mode string

const (
	ModeTable Mode = "table"
	ModePlain Mode = "plain"
	ModeJSON  Mode = "json"
)

// Printer dispatches a Renderable to stdout in the active Mode.
type Printer struct {
	mode   Mode
	stdout io.Writer
}

func NewPrinter(mode Mode) *Printer {
	return &Printer{mode: mode, stdout: tuioutput.Stdout()}
}

func (p *Printer) Mode() Mode { return p.mode }

// Print writes the Renderable to stdout in the printer's active Mode.
// JSON bytes are written verbatim with a trailing newline appended when
// the renderer did not include one.
func (p *Printer) Print(r Renderable) error {
	switch p.mode {
	case ModeJSON:
		b, err := r.RenderJSON()
		if err != nil {
			return fmt.Errorf("tui: render json: %w", err)
		}
		if _, err := p.stdout.Write(b); err != nil {
			return err
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			if _, err := p.stdout.Write([]byte("\n")); err != nil {
				return err
			}
		}
		return nil
	case ModePlain:
		_, err := fmt.Fprintln(p.stdout, r.RenderPlain())
		return err
	default:
		_, err := fmt.Fprintln(p.stdout, r.RenderTable())
		return err
	}
}

// ParseMode validates a user-supplied --output value. Empty input
// auto-detects from dry/tui's terminal/agent/CI heuristics.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeTable, ModePlain, ModeJSON:
		return Mode(s), nil
	case "":
		return autoDetect(), nil
	default:
		return "", fmt.Errorf("unknown --output %q: must be table, plain, or json", s)
	}
}

// AutoMode returns the auto-detected Mode for the current environment.
// dry/tui agent mode maps to JSON for data output; rich and plain map
// to their named equivalents.
func AutoMode() Mode { return autoDetect() }

func autoDetect() Mode {
	switch tuioutput.CurrentMode() {
	case tuioutput.Plain:
		return ModePlain
	case tuioutput.Agent:
		return ModeJSON
	default:
		return ModeTable
	}
}
