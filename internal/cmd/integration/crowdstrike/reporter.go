package crowdstrike

import (
	"encoding/json"

	"github.com/safedep/cli/internal/tui"
	"github.com/safedep/dry/log"
	drytui "github.com/safedep/dry/tui"
)

// eventPackageBlocked is the single -o json event name. Both block actions
// (malicious and cooldown) share it, distinguished by the action field.
const eventPackageBlocked = "endpoint_package_blocked"

// cooldownJSON is the nested cooldown detail, present only on cooldown events.
type cooldownJSON struct {
	PublishDate      string `json:"publish_date,omitempty"`
	CooldownDays     uint32 `json:"cooldown_days,omitempty"`
	DaysSincePublish uint32 `json:"days_since_publish,omitempty"`
	DaysRemaining    uint32 `json:"days_remaining,omitempty"`
}

// jsonEvent is one JSONL record on stdout under -o json. Fields are omitempty so
// each record carries only what it has.
type jsonEvent struct {
	Event      string        `json:"event"`
	Action     string        `json:"action,omitempty"`
	EventID    string        `json:"event_id,omitempty"`
	Timestamp  string        `json:"timestamp,omitempty"`
	EndpointID string        `json:"endpoint_id,omitempty"`
	Endpoint   string        `json:"endpoint_name,omitempty"`
	Tool       string        `json:"tool_name,omitempty"`
	Package    string        `json:"package,omitempty"`
	Ecosystem  string        `json:"ecosystem,omitempty"`
	Version    string        `json:"version,omitempty"`
	IsMalware  bool          `json:"is_malware,omitempty"`
	IsVerified bool          `json:"is_verified,omitempty"`
	AnalysisID string        `json:"analysis_id,omitempty"`
	Cooldown   *cooldownJSON `json:"cooldown,omitempty"`
}

func (e jsonEvent) RenderJSON() ([]byte, error) { return json.Marshal(e) }
func (e jsonEvent) RenderTable() string         { return e.Event }
func (e jsonEvent) RenderPlain() string         { return e.Event }

// reporter sends daemon activity to the right stream for the active output
// mode. With -o json the user asked for machine output, so it writes only
// result events as JSONL on stdout and drops every log line. In any other mode
// it writes nothing to stdout and sends results and logs to stderr as drytui
// lines, the same as the rest of the CLI.
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
			log.Warnf("integration crowdstrike: emit json event: %v", err)
		}
		return
	}
	human()
}

// The log* methods are for operational messages. They print to stderr in human
// modes and print nothing under -o json.

func (r *reporter) logInfo(format string, a ...any) {
	if r.json {
		return
	}
	drytui.Info(format, a...)
}

func (r *reporter) logWarn(format string, a ...any) {
	if r.json {
		return
	}
	drytui.Warning(format, a...)
}
