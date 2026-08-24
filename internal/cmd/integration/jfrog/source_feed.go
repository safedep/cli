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

// feedSource is a packageSource backed by the SafeDep ThreatIntel Feed
// (ThreatIntelService.ListPackageReports). It runs an interval loop,
// sleeping pollInterval between drains, and uses a profile-scoped KV
// cursor (see store.go) to resume across restarts.
//
// The cursor watermark is the max updated_at seen. Unlike a creation-time
// list, the feed re-delivers a report on every material change (a
// suspicious to malicious upgrade, a new affected version, a withdrawal),
// so an updated_at cursor never moves past a report before it is verified.
type feedSource struct {
	svc            threatintelv1grpc.ThreatIntelServiceClient
	cursor         *cursorStore
	pollInterval   time.Duration
	backfillWindow time.Duration
}

const (
	// feedPageSize is the number of reports requested per page. The server
	// caps reports at 100, so ask for exactly that.
	feedPageSize = 100
)

func newFeedSource(svc threatintelv1grpc.ThreatIntelServiceClient, kv *storage.KV[cursorState], pollInterval, backfillWindow time.Duration) *feedSource {
	return &feedSource{
		svc:            svc,
		cursor:         newCursorStore(kv),
		pollInterval:   pollInterval,
		backfillWindow: backfillWindow,
	}
}

// subscribe drives the feed loop until ctx is cancelled.
//
// Per-cycle errors (gRPC failures, transient network, cursor save
// failures) are surfaced via drytui.Warning and the loop continues. A
// single bad cycle must not bring down the daemon. Cancellation between
// cycles is honoured immediately.
func (s *feedSource) subscribe(ctx context.Context, onRecord recordHandler) error {
	drytui.Info("Starting JFrog threat intel feed (interval: %s)", s.pollInterval)

	for {
		err := s.syncOnce(ctx, onRecord)
		switch {
		case err == nil:
			drytui.Info("Feed cycle complete, next in %s", s.pollInterval)
		case ctx.Err() != nil:
			return nil
		case isCallbackError(err):
			// Per the recordHandler contract, callback errors must surface
			// from Subscribe. Unwrap so the caller sees the original error,
			// not our internal wrapper.
			return errors.Unwrap(err)
		default:
			// Transient infrastructure error (gRPC blip, network reset, cursor
			// save failure). Log and retry on the next cycle.
			drytui.Warning("Feed cycle error: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.pollInterval):
		}
	}
}

// syncOnce drains the feed once: it pages through every malicious report
// newer than the cursor and calls onRecord for each one, advancing the
// cursor as it goes.
//
// since semantics (ThreatIntel Feed):
//   - filters.since is a STRICT > filter on updated_at.
//   - First run (no cursor): since = now - backfillWindow. With the
//     default backfill of 0 this is now, i.e. a fresh start. since is
//     never omitted: an omitted filter would pull the full feed history.
//   - since is fixed for the whole drain. Only the page token advances.
//   - Reports are requested in ascending updated_at order so an
//     interrupted drain resumes without gaps.
func (s *feedSource) syncOnce(ctx context.Context, onRecord recordHandler) error {
	state, err := s.cursor.load(ctx)
	if err != nil {
		return fmt.Errorf("feed: load cursor: %w", err)
	}

	since := state.LastSeenAt
	if since.IsZero() {
		since = time.Now().UTC().Add(-s.backfillWindow)
	}

	// maxSeen is the watermark for this drain. It starts at the loaded
	// cursor so it only ever moves forward, and it counts withdrawn
	// reports too so the cursor advances past retractions.
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
				// Wrap so subscribe can tell this apart from infra errors
				// and surface it instead of logging and retrying.
				return &callbackError{err: err}
			}
			if t := report.GetUpdatedAt(); t != nil {
				if ts := t.AsTime(); ts.After(maxSeen) {
					maxSeen = ts
				}
			}
		}

		// Persist after each page so a crash mid-drain resumes from the
		// last page, not the start. Forward-only: never move the cursor
		// backward.
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
