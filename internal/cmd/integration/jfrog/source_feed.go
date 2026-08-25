package jfrog

import (
	"context"
	"errors"
	"fmt"
	"time"

	threatintelv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/threatintel/v1/threatintelv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	threatintelsvcv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/threatintel/v1"
	"github.com/safedep/cli/internal/storage"
	drytui "github.com/safedep/dry/tui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
}

// feedPageSize matches the server's cap of 100 reports per page.
const feedPageSize = 100

func newFeedSource(svc threatintelv1grpc.ThreatIntelServiceClient, kv *storage.KV[cursorState], pollInterval, backfillWindow time.Duration) *feedSource {
	return &feedSource{
		svc:            svc,
		cursor:         newCursorStore(kv),
		pollInterval:   pollInterval,
		backfillWindow: backfillWindow,
	}
}

// subscribe drives the feed loop until ctx is cancelled. A bad cycle is logged
// and retried, never fatal.
func (s *feedSource) subscribe(ctx context.Context, onRecord recordHandler) error {
	drytui.Info("Starting JFrog Syncing with SafeDep Threat Intel Feed")
	s.logStartMode(ctx)

	for {
		err := s.syncOnce(ctx, onRecord)
		switch {
		case err == nil:
			drytui.Info("Feed cycle complete at %s, next in %s", time.Now().UTC().Format(time.RFC3339), s.pollInterval)
		case ctx.Err() != nil:
			return nil
		case isCallbackError(err):
			// A handler error must surface. Unwrap the internal wrapper first.
			return errors.Unwrap(err)
		default:
			// Transient infra error. Log and retry next cycle.
			drytui.Warning("Feed cycle error: %v", err)
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
		drytui.Info("Resuming from saved cursor (last update %s)", state.LastSeenAt.UTC().Format(time.RFC3339))
	case s.backfillWindow > 0:
		drytui.Info("No saved cursor: backfilling reports from the last %s", s.backfillWindow)
	default:
		drytui.Info("No saved cursor: starting fresh from now")
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
			return fmt.Errorf("feed: list reports: %w", err)
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

		// Persist per page so a mid-drain crash resumes from here, forward-only.
		if maxSeen.After(state.LastSeenAt) {
			if err := s.cursor.save(ctx, cursorState{LastSeenAt: maxSeen}); err != nil {
				return fmt.Errorf("feed: save cursor: %w", err)
			}
			state.LastSeenAt = maxSeen
		}

		nextToken := resp.GetPagination().GetNextPageToken()
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	return nil
}
