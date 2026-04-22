package output

import (
	"encoding/json"
	"fmt"

	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/output"
)

type Format string

const (
	FormatTable Format = "table"
	FormatPlain Format = "plain"
	FormatJSON  Format = "json"
)

// Renderable is implemented by data commands that support structured output.
// Operational commands (login, install) use tui.Info/Success/Error directly.
type Renderable interface {
	RenderJSON() ([]byte, error)
	RenderTable() string
	RenderPlain() string
}

type Formatter struct {
	format Format
}

func New(format Format) *Formatter {
	return &Formatter{format: format}
}

// DefaultFormat returns the appropriate default based on TTY state.
func DefaultFormat() Format {
	switch output.CurrentMode() {
	case output.Plain, output.Agent:
		return FormatPlain
	default:
		return FormatTable
	}
}

func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatPlain, FormatJSON:
		return Format(s), nil
	case "":
		return DefaultFormat(), nil
	default:
		return "", fmt.Errorf("unknown output format %q: must be table, plain, or json", s)
	}
}

func (f *Formatter) Print(r Renderable) error {
	switch f.format {
	case FormatJSON:
		b, err := r.RenderJSON()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output.Stdout(), string(b))
		return err
	case FormatPlain:
		_, err := fmt.Fprintln(output.Stdout(), r.RenderPlain())
		return err
	default:
		_, err := fmt.Fprintln(output.Stdout(), r.RenderTable())
		return err
	}
}

// PrintJSON marshals any value to indented JSON on stdout.
// Used when a command doesn't implement Renderable but -o json is set.
func (f *Formatter) PrintJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output.Stdout(), string(b))
	return err
}

func (f *Formatter) Info(format string, a ...any)    { tui.Info(format, a...) }
func (f *Formatter) Success(format string, a ...any) { tui.Success(format, a...) }
func (f *Formatter) Warning(format string, a ...any) { tui.Warning(format, a...) }
func (f *Formatter) Error(format string, a ...any)   { tui.Error(format, a...) }
func (f *Formatter) Format() Format                   { return f.format }
