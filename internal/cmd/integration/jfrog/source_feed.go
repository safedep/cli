package jfrog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	threatintelv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/threatintel/v1/threatintelv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	threatintelsvcv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/threatintel/v1"
	"github.com/safedep/cli/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// errFeedNotEntitled marks the feed rejecting the tenant for lack of the Threat
// Intel Feed add-on. It is not transient, so subscribe stops with a friendly
// message instead of retrying every cycle.
var errFeedNotEntitled = errors.New(
	"the SafeDep JFrog XRay integration is available with the Threat Intel Feed add-on, " +
		"which is not enabled for this tenant. See the pricing page: https://safedep.io/pricing/#threat-intel")

// errFeedAuth marks the data-plane rejecting the SafeDep credential (wrong or
// missing API key for the tenant, often after an OAuth-only login). Not
// transient, so subscribe stops with how to authenticate for the feed.
var errFeedAuth = errors.New(
	"SafeDep authentication failed for the Threat Intel Feed: the feed reads the data plane " +
		"(api.safedep.io) and needs an API key for your tenant, so log in with " +
		"'safedep auth login --tenant <tenant>.safedep.io --api-key --api-key-value <API_KEY>' " +
		"or set SAFEDEP_API_KEY and SAFEDEP_TENANT_ID")

// isFeedAuthError reports whether the feed rejected our SafeDep credential. The
// server returns Unauthenticated, or wraps the auth failure in an Internal
// status whose message names the API key.
func isFeedAuthError(err error) bool {
	if status.Code(err) == codes.Unauthenticated {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "api key auth failed") || strings.Contains(msg, "does not belong to tenant")
}

// feedSource pulls malicious package reports from the ThreatIntel Feed on an
// interval, resuming from a profile-scoped cursor (see store.go).
//
// The cursor is the max updated_at seen. The feed re-delivers a report on any
// change (verification, new version, withdrawal), so unlike a created_at list
// the cursor never skips a report before it is verified.
type feedSource struct {
	svc            threatintelv1grpc.ThreatIntelServiceClient
	cursor         *cursorStore
	pollInterval   time.Duration
	backfillWindow time.Duration
	rep            *reporter
}

// feedPageSize matches the server's cap of 100 reports per page.
const feedPageSize = 100

func newFeedSource(svc threatintelv1grpc.ThreatIntelServiceClient, kv *storage.KV[cursorState], pollInterval, backfillWindow time.Duration, rep *reporter) *feedSource {
	return &feedSource{
		svc:            svc,
		cursor:         newCursorStore(kv),
		pollInterval:   pollInterval,
		backfillWindow: backfillWindow,
		rep:            rep,
	}
}

// subscribe drives the feed loop until ctx is cancelled. A bad cycle is logged
// and retried, never fatal.
func (s *feedSource) subscribe(ctx context.Context, onRecord recordHandler) error {
	// Feed-loop activity is operational. It is a log: stderr in human modes,
	// nothing under -o json. It is never output.
	s.rep.logInfo("Starting JFrog Syncing with SafeDep Threat Intel Feed")
	s.logStartMode(ctx)

	for {
		err := s.syncOnce(ctx, onRecord)
		switch {
		case err == nil:
			s.rep.logInfo("Feed cycle complete at %s, next in %s", time.Now().UTC().Format(time.RFC3339), s.pollInterval)
		case ctx.Err() != nil:
			return nil
		case isCallbackError(err):
			// A handler error must surface. Unwrap the internal wrapper first.
			return errors.Unwrap(err)
		case errors.Is(err, errFeedNotEntitled), errors.Is(err, errFeedAuth):
			// Not transient (missing add-on or bad credential). Stop, don't loop.
			return err
		default:
			// Transient infra error. Log and retry next cycle.
			s.rep.logWarn("Feed cycle error: %v", err)
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
func (s *feedSource) logStartMode(ctx context.Context) {
	state, err := s.cursor.load(ctx)
	if err != nil {
		return
	}
	switch {
	case !state.LastSeenAt.IsZero():
		s.rep.logInfo("Resuming from saved cursor (last update %s)", state.LastSeenAt.UTC().Format(time.RFC3339))
	case s.backfillWindow > 0:
		s.rep.logInfo("No saved cursor: backfilling reports from the last %s", s.backfillWindow)
	default:
		s.rep.logInfo("No saved cursor: starting fresh from now")
	}
}

// syncOnce pages through every malicious report newer than the cursor,
// delivering each and advancing the cursor.
//
// since is a strict > filter on updated_at, fixed for the whole drain (only the
// page token moves). First run uses now - backfill; it is never omitted, since
// that would pull the full history. Ascending order lets an interrupted drain
// resume without gaps.
func (s *feedSource) syncOnce(ctx context.Context, onRecord recordHandler) error {
	state, err := s.cursor.load(ctx)
	if err != nil {
		return fmt.Errorf("feed: load cursor: %w", err)
	}

	since := state.LastSeenAt
	if since.IsZero() {
		since = time.Now().UTC().Add(-s.backfillWindow)
	}

	// Watermark starts at the cursor so it only moves forward, and counts
	// withdrawn reports too so it advances past retractions.
	maxSeen := state.LastSeenAt

	var pageToken string
	for {
		req := &threatintelsvcv1.ListPackageReportsRequest{}

		filters := &threatintelsvcv1.ListPackageReportsRequest_Filters{}
		filters.SetSince(timestamppb.New(since))
		filters.SetVerdict(threatintelv1.ThreatVerdict_THREAT_VERDICT_MALICIOUS)
		req.SetFilters(filters)

		pagination := &controltowerv1.PaginationRequest{}
		pagination.SetPageSize(feedPageSize)
		pagination.SetSortOrder(controltowerv1.PaginationRequest_SORT_ORDER_ASCENDING)
		if pageToken != "" {
			pagination.SetPageToken(pageToken)
		}
		req.SetPagination(pagination)

		resp, err := s.svc.ListPackageReports(ctx, req)
		if err != nil {
			switch {
			case status.Code(err) == codes.PermissionDenied:
				return errFeedNotEntitled
			case isFeedAuthError(err):
				return errFeedAuth
			default:
				return fmt.Errorf("feed: list reports: %w", err)
			}
		}

		for _, report := range resp.GetPackageReports() {
			if err := onRecord(report); err != nil {
				// Wrap so subscribe surfaces it instead of retrying.
				return &callbackError{err: err}
			}
			if t := report.GetUpdatedAt(); t != nil {
				if ts := t.AsTime(); ts.After(maxSeen) {
					maxSeen = ts
				}
			}
		}

		nextToken := resp.GetPagination().GetNextPageToken()
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	// Persist once, after the full drain. Saving per page would strand reports
	// that share an updated_at across a page boundary: a crash after saving that
	// timestamp makes the next run's strict > since skip the rest.
	switch {
	case maxSeen.After(state.LastSeenAt):
		if err := s.cursor.save(ctx, cursorState{LastSeenAt: maxSeen}); err != nil {
			return fmt.Errorf("feed: save cursor: %w", err)
		}
	case state.LastSeenAt.IsZero():
		// First run that saw nothing new: anchor since so the next cycle resumes
		// from it. Otherwise since slides forward by pollInterval each cycle and
		// misses reports updated inside the gap.
		if err := s.cursor.save(ctx, cursorState{LastSeenAt: since}); err != nil {
			return fmt.Errorf("feed: save cursor: %w", err)
		}
	}

	return nil
}
