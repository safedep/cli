// Package tui owns the CLI's data-presentation contract and the theme
// it pushes into dry/tui at startup. Operational messaging
// (Info / Success / Warning / Error) is not wrapped here. Commands call
// the dry/tui helpers directly for those.
package tui

// Renderable is implemented by command results that produce structured
// output. Each method emits the same data in a different format. The
// dispatcher (Printer.Print) picks one based on the active Mode.
//
// Lists implement the same interface. RenderTable produces a table,
// RenderPlain emits a line per item, RenderJSON returns the encoded
// array. Pre-built helpers for common shapes are not provided yet.
type Renderable interface {
	RenderJSON() ([]byte, error)
	RenderTable() string
	RenderPlain() string
}
