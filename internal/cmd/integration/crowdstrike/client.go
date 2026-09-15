package crowdstrike

import (
	"context"
	"strings"
	"time"

	ctmsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// eventSink is the port each new endpoint block event is sent through. The
// service never knows the destination. printClient (Stage 1) logs the event; a
// future crowdstrikeClient (Stage 2) will POST it to the CrowdStrike SIEM
// ingest API. Swapping the adapter is the only change needed, exactly like
// jfrog's xrayClient.
type eventSink interface {
	// validate proves the sink is reachable/authorized, once at startup.
	validate(ctx context.Context) error
	// send delivers one event. The adapter owns mapping and emitting its own
	// result line. Errors are best-effort (logged, non-fatal) in the service.
	send(ctx context.Context, event *packageGuardEvent) error
}

const (
	actionBlocked         = "blocked"
	actionCooldownBlocked = "cooldown_blocked"
	actionUnknown         = "unknown"
)

// toJSONEvent maps a package-guard event to the neutral record shared by every
// sink adapter. ok is false when the event carries no package decision
// (defensive: the server-side filter should guarantee one).
func toJSONEvent(ev *packageGuardEvent) (jsonEvent, bool) {
	dec := ev.GetPmgEvent().GetPackageDecision()
	if dec == nil {
		return jsonEvent{}, false
	}

	pkg := dec.GetPackageVersion()
	out := jsonEvent{
		Event:      eventPackageBlocked,
		Action:     actionString(dec.GetAction()),
		EventID:    ev.GetEventId(),
		Timestamp:  formatTime(ev.GetTimestamp()),
		EndpointID: ev.GetEndpointId(),
		Endpoint:   ev.GetEndpointName(),
		Tool:       ev.GetToolName(),
		Package:    pkg.GetPackage().GetName(),
		Ecosystem:  ecosystemString(pkg.GetPackage().GetEcosystem()),
		Version:    pkg.GetVersion(),
		IsMalware:  dec.GetIsMalware(),
		IsVerified: dec.GetIsVerified(),
		AnalysisID: dec.GetAnalysisId(),
	}
	if cd := dec.GetCooldown(); cd != nil {
		out.Cooldown = &cooldownJSON{
			PublishDate:      formatTime(cd.GetPublishDate()),
			CooldownDays:     cd.GetCooldownDays(),
			DaysSincePublish: cd.GetDaysSincePublish(),
			DaysRemaining:    cd.GetDaysRemaining(),
		}
	}
	return out, true
}

// actionString maps the PMG package action to the stable string used in output.
// Only the two block actions are requested from the server, so anything else is
// unexpected and tagged unknown rather than dropped.
func actionString(a ctmsgv1.PmgPackageAction) string {
	switch a {
	case ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED:
		return actionBlocked
	case ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED:
		return actionCooldownBlocked
	default:
		return actionUnknown
	}
}

// ecosystemString renders a SafeDep ecosystem as a lowercase name (npm, pypi,
// ...). It derives the name from the enum so a new SafeDep ecosystem needs no
// code change here.
func ecosystemString(e packagev1.Ecosystem) string {
	return strings.ToLower(strings.TrimPrefix(e.String(), "ECOSYSTEM_"))
}

// formatTime renders a timestamp as RFC3339 UTC, or "" when absent.
func formatTime(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}
