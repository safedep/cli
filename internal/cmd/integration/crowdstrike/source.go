package crowdstrike

import (
	"context"
	"errors"
	"fmt"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	ctmsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	ctsvcv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/cli/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// packageGuardEvent aliases the deeply-nested generated event type so the rest
// of the package reads cleanly.
type packageGuardEvent = ctsvcv1.ListEndpointPackageGuardEventsResponse_PackageGuardEvent

// recordHandler handles one event. A non-nil error stops delivery and surfaces
// from subscribe.
type recordHandler func(*packageGuardEvent) error

// callbackError marks a handler error so the source can tell it apart from a
// transient infra error: the former surfaces, the latter is retried. Wrapped
// inside the source, unwrapped at the subscribe boundary.
type callbackError struct{ err error }

func (e *callbackError) Error() string { return e.err.Error() }
func (e *callbackError) Unwrap() error { return e.err }

func isCallbackError(err error) bool {
	var cb *callbackError
	return errors.As(err, &cb)
}

// errNotEntitled marks the server rejecting the tenant for lack of the endpoint
// management add-on. Not transient, so subscribe stops instead of retrying.
var errNotEntitled = errors.New(
	"the SafeDep CrowdStrike endpoint integration requires the endpoint management add-on, " +
		"which is not enabled for this tenant. See the pricing page: https://safedep.io/pricing")

// errAuth marks the control plane rejecting the SafeDep credential (missing or
// expired OAuth session). Not transient, so subscribe stops with how to
// authenticate.
var errAuth = errors.New(
	"SafeDep authentication failed: this integration reads the control plane (cloud.safedep.io), " +
		"so log in with 'safedep auth login' (OAuth device login) and retry")

// isAuthError reports whether the server rejected our SafeDep credential. The
// control plane returns Unauthenticated for a missing or expired token.
func isAuthError(err error) bool {
	return status.Code(err) == codes.Unauthenticated
}

// eventPageSize matches the server's per-page cap.
const eventPageSize = 100

// eventSource pulls endpoint package-guard block events on an interval,
// resuming from a profile-scoped cursor (see store.go). Events are immutable and
// append-only, so a Timestamp watermark never skips an event before it is seen.
type eventSource struct {
	svc            controltowerv1grpc.EndpointManagementServiceClient
	cursor         *cursorStore
	pollInterval   time.Duration
	backfillWindow time.Duration
	rep            *reporter
}

func newEventSource(svc controltowerv1grpc.EndpointManagementServiceClient, kv *storage.KV[cursorState], pollInterval, backfillWindow time.Duration, rep *reporter) *eventSource {
	return &eventSource{
		svc:            svc,
		cursor:         newCursorStore(kv),
		pollInterval:   pollInterval,
		backfillWindow: backfillWindow,
		rep:            rep,
	}
}

// subscribe drives the sync loop until ctx is cancelled. A bad cycle is logged
// and retried, never fatal, except an add-on/auth failure or a handler error.
func (s *eventSource) subscribe(ctx context.Context, onRecord recordHandler) error {
	s.rep.logInfo("Starting CrowdStrike endpoint block event sync with SafeDep")
	s.logStartMode(ctx)

	for {
		err := s.syncOnce(ctx, onRecord)
		switch {
		case err == nil:
			s.rep.logInfo("Sync cycle complete at %s, next in %s", time.Now().UTC().Format(time.RFC3339), s.pollInterval)
		case ctx.Err() != nil:
			return nil
		case isCallbackError(err):
			return errors.Unwrap(err)
		case errors.Is(err, errNotEntitled), errors.Is(err, errAuth):
			return err
		default:
			s.rep.logWarn("Sync cycle error: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.pollInterval):
		}
	}
}

// logStartMode tells the operator, once at startup, whether we resume or start
// fresh. A load error here is ignored: syncOnce surfaces the real one.
func (s *eventSource) logStartMode(ctx context.Context) {
	state, err := s.cursor.load(ctx)
	if err != nil {
		return
	}
	switch {
	case !state.LastSeenAt.IsZero():
		s.rep.logInfo("Resuming from saved cursor (last event %s)", state.LastSeenAt.UTC().Format(time.RFC3339))
	case s.backfillWindow > 0:
		s.rep.logInfo("No saved cursor: backfilling events from the last %s", s.backfillWindow)
	default:
		s.rep.logInfo("No saved cursor: starting fresh from now")
	}
}

// syncOnce pages through every block event newer than the cursor, delivering
// each and advancing the cursor.
//
// TimeRange.Start is the watermark, fixed for the whole drain (only the page
// token moves). First run uses now - backfill; it is never omitted. Ascending
// order lets an interrupted drain resume without gaps. Events whose id was at
// exactly the cursor timestamp are skipped, so an inclusive Start never
// re-delivers the boundary.
func (s *eventSource) syncOnce(ctx context.Context, onRecord recordHandler) error {
	state, err := s.cursor.load(ctx)
	if err != nil {
		return fmt.Errorf("crowdstrike: load cursor: %w", err)
	}

	since := state.LastSeenAt
	if since.IsZero() {
		since = time.Now().UTC().Add(-s.backfillWindow)
	}

	// The API requires an end and rejects start >= end. Cap the window at now,
	// fixed for the whole drain (only the page token moves). The guard covers a
	// fresh start where since == now: end is nudged past it. Fetching up to a
	// moment in the future is harmless, no events exist there.
	end := time.Now().UTC()
	if !end.After(since) {
		end = since.Add(time.Second)
	}

	skip := make(map[string]struct{}, len(state.LastSeenEventIDs))
	for _, id := range state.LastSeenEventIDs {
		skip[id] = struct{}{}
	}

	// Watermark starts at the cursor so it only moves forward. newMaxIDs collects
	// the ids at newMax, to become the next boundary skip set.
	newMax := state.LastSeenAt
	var newMaxIDs []string

	var pageToken string
	for {
		resp, err := s.svc.ListEndpointPackageGuardEvents(ctx, buildRequest(since, end, pageToken))
		if err != nil {
			switch {
			case status.Code(err) == codes.PermissionDenied:
				return errNotEntitled
			case isAuthError(err):
				return errAuth
			default:
				return fmt.Errorf("crowdstrike: list events: %w", err)
			}
		}

		for _, ev := range resp.GetEvents() {
			id := ev.GetEventId()
			if _, dup := skip[id]; dup {
				continue // boundary dedup: already processed at the cursor timestamp
			}
			if err := onRecord(ev); err != nil {
				return &callbackError{err: err}
			}

			ts := ev.GetTimestamp()
			if ts == nil {
				continue
			}
			switch t := ts.AsTime(); {
			case t.After(newMax):
				newMax = t
				newMaxIDs = []string{id}
			case t.Equal(newMax):
				newMaxIDs = append(newMaxIDs, id)
			}
		}

		nextToken := resp.GetPagination().GetNextPageToken()
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	// Persist once, after the full drain. Saving per page would strand events
	// that share a timestamp across a page boundary.
	switch {
	case newMax.After(state.LastSeenAt):
		if err := s.cursor.save(ctx, cursorState{LastSeenAt: newMax, LastSeenEventIDs: newMaxIDs}); err != nil {
			return fmt.Errorf("crowdstrike: save cursor: %w", err)
		}
	case newMax.Equal(state.LastSeenAt) && len(newMaxIDs) > 0:
		// New events at the same boundary timestamp: extend the skip set so they
		// are not re-delivered next cycle. Rare with strictly increasing event
		// times. ponytail: bounded by real events at one timestamp.
		merged := append(append([]string{}, state.LastSeenEventIDs...), newMaxIDs...)
		if err := s.cursor.save(ctx, cursorState{LastSeenAt: newMax, LastSeenEventIDs: merged}); err != nil {
			return fmt.Errorf("crowdstrike: save cursor: %w", err)
		}
	case state.LastSeenAt.IsZero():
		// First run that saw nothing new: anchor since so the next cycle resumes
		// from it instead of sliding forward by pollInterval each cycle.
		if err := s.cursor.save(ctx, cursorState{LastSeenAt: since}); err != nil {
			return fmt.Errorf("crowdstrike: save cursor: %w", err)
		}
	}

	return nil
}

// buildRequest constructs one page request: the two block actions, ascending by
// event time, over the [since, end] window.
func buildRequest(since, end time.Time, pageToken string) *ctsvcv1.ListEndpointPackageGuardEventsRequest {
	req := &ctsvcv1.ListEndpointPackageGuardEventsRequest{}

	tr := &ctsvcv1.EndpointManagementTimeRange{}
	tr.SetStart(timestamppb.New(since))
	tr.SetEnd(timestamppb.New(end))
	req.SetTimeRange(tr)

	pmg := &ctsvcv1.ListEndpointPackageGuardEventsRequest_Filter_PmgFilter{}
	pmg.SetEventTypes([]ctmsgv1.PmgEventType{ctmsgv1.PmgEventType_PMG_EVENT_TYPE_PACKAGE_DECISION})
	pmg.SetPackageActions([]ctmsgv1.PmgPackageAction{
		ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED,
		ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED,
	})
	filter := &ctsvcv1.ListEndpointPackageGuardEventsRequest_Filter{}
	filter.SetPmg(pmg)
	req.SetFilter(filter)

	pagination := &ctmsgv1.PaginationRequest{}
	pagination.SetPageSize(eventPageSize)
	pagination.SetSortOrder(ctmsgv1.PaginationRequest_SORT_ORDER_ASCENDING)
	if pageToken != "" {
		pagination.SetPageToken(pageToken)
	}
	req.SetPagination(pagination)

	return req
}
