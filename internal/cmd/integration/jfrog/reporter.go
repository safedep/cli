package jfrog

import (
	"encoding/json"
	"fmt"

	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	"github.com/safedep/cli/internal/tui"
	"github.com/safedep/dry/log"
	drytui "github.com/safedep/dry/tui"
	tuioutput "github.com/safedep/dry/tui/output"
	"github.com/safedep/dry/tui/style"
)

// jsonEvent names for the -o json output stream. Only user-facing results reach
// this stream. A result is a real state change in XRay, or a dry-run preview of
// one.
const (
	eventPushed       = "package_pushed"
	eventDeleted      = "package_deleted"
	eventDryRunPush   = "dry_run_package_push"
	eventDryRunDelete = "dry_run_package_delete"
)

// jsonEvent is one JSONL record on stdout. Fields are omitempty so each record
// carries only what it has.
type jsonEvent struct {
	Event     string   `json:"event"`
	ReportID  string   `json:"report_id,omitempty"`
	Package   string   `json:"package,omitempty"`
	Ecosystem string   `json:"ecosystem,omitempty"`
	Versions  []string `json:"versions,omitempty"`
	IssueID   string   `json:"issue_id,omitempty"`
	Status    int      `json:"status,omitempty"`
}

func (e jsonEvent) RenderJSON() ([]byte, error) { return json.Marshal(e) }
func (e jsonEvent) RenderTable() string         { return e.Event }
func (e jsonEvent) RenderPlain() string         { return e.Event }

// reporter sends daemon activity to the right stream for the active output
// mode. With -o json the user asked for machine output. So it writes only result
// events, as JSONL on stdout, and drops every log line. In any other mode it
// writes nothing to stdout. It sends results and logs to stderr as drytui lines,
// the same as the rest of the CLI.
type reporter struct {
	out  *tui.Printer
	json bool
}

func newReporter(out *tui.Printer) *reporter {
	return &reporter{out: out, json: out != nil && out.Mode() == tui.ModeJSON}
}

// result reports one user-facing result. With -o json it writes a JSONL record
// to stdout. In any other mode it runs the human drytui line.
func (r *reporter) result(human func(), ev jsonEvent) {
	if r.json {
		if err := r.out.Print(ev); err != nil {
			// A stdout write failure is not actionable by the operator, but it
			// must not be swallowed silently.
			log.Warnf("integration jfrog: emit json event: %v", err)
		}
		return
	}
	human()
}

// The log* methods are for operational messages. They print to stderr in human
// modes. They print nothing under -o json.

func (r *reporter) logInfo(format string, a ...any) {
	if r.json {
		return
	}
	drytui.Info(format, a...)
}

func (r *reporter) logSuccess(format string, a ...any) {
	if r.json {
		return
	}
	drytui.Success(format, a...)
}

func (r *reporter) logWarn(format string, a ...any) {
	if r.json {
		return
	}
	drytui.Warning(format, a...)
}

// logDim prints a dimmed line at normal verbosity. drytui.Faint shows a line
// only with --verbose. logDim shows it always in human modes. Use it for
// frequent no-ops that should stay visible but quiet.
func (r *reporter) logDim(format string, a ...any) {
	if r.json {
		return
	}
	_, _ = fmt.Fprintln(tuioutput.Stderr(), style.Faint(fmt.Sprintf(format, a...)))
}

// logSkip reports a skipped report identically from both clients.
func logSkip(r *reporter, report *threatintelv1.PackageReport, reason string) {
	r.logWarn("Skipping report %s: %s", report.GetReportId(), reason)
}
