package crowdstrike

import (
	"context"
	"fmt"

	drytui "github.com/safedep/dry/tui"
)

var _ eventSink = (*printClient)(nil)

// printClient is the Stage 1 sink adapter. It maps each event with the shared
// toJSONEvent and logs it via the reporter instead of sending it anywhere. When
// the CrowdStrike sink lands (Stage 2) it implements the same eventSink and is
// swapped in, changing nothing in the source or service.
type printClient struct {
	rep *reporter
}

func newPrintClient(rep *reporter) *printClient { return &printClient{rep: rep} }

func (c *printClient) validate(_ context.Context) error {
	c.rep.logInfo("Logging mode: printing endpoint block events, nothing is sent to an external system")
	return nil
}

func (c *printClient) send(_ context.Context, ev *packageGuardEvent) error {
	rec, ok := toJSONEvent(ev)
	if !ok {
		c.rep.logWarn("Skipping event %s: no package decision", ev.GetEventId())
		return nil
	}

	c.rep.result(func() { drytui.Warning("%s", humanLine(rec)) }, rec)
	return nil
}

// humanLine renders one event for the human log stream.
func humanLine(rec jsonEvent) string {
	pkg := rec.Package
	if rec.Version != "" {
		pkg = rec.Package + "@" + rec.Version
	}

	if rec.Action == actionCooldownBlocked {
		detail := "cooldown"
		if rec.Cooldown != nil {
			detail = fmt.Sprintf("cooldown, %dd remaining", rec.Cooldown.DaysRemaining)
		}
		return fmt.Sprintf("Cooldown block: %s (%s) %s on %s [%s]", pkg, rec.Ecosystem, detail, endpointLabel(rec), rec.EventID)
	}

	kind := "blocked"
	if rec.IsMalware {
		kind = "malware"
	}
	return fmt.Sprintf("Malicious block: %s (%s) %s on %s [%s]", pkg, rec.Ecosystem, kind, endpointLabel(rec), rec.EventID)
}

// endpointLabel prefers the human endpoint name, falling back to its id.
func endpointLabel(rec jsonEvent) string {
	if rec.Endpoint != "" {
		return rec.Endpoint
	}
	return rec.EndpointID
}
