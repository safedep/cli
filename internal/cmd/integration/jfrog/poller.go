package jfrog

import (
	"context"
	"fmt"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	drytui "github.com/safedep/dry/tui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// maliciousPackagePoller drives one poll cycle against SafeDep's malware
// analysis API. It is stateless except for the file-backed cursor — the
// daemon loop in feedService is responsible for invoking it on a schedule.
type maliciousPackagePoller struct {
	svc    malysisv1grpc.MalwareAnalysisServiceClient
	cursor *cursorStore
}

const (
	// pollPageSize is the number of records requested per page. Tuned for a
	// balance between API roundtrips and memory: at 100 a single page is small
	// enough to keep memory bounded and large enough to make pagination cheap.
	pollPageSize = 100

	// apiCutoffAge is the maximum age the SafeDep API accepts for start_from.
	// Requests with start_from older than this are rejected with an error
	// ("startFrom is before cutoff date") rather than silently clamped.
	apiCutoffAge = 7 * 24 * time.Hour

	// safeStartFromAge is the age we reset to when the stored cursor has
	// fallen outside the cutoff window (e.g. daemon was down >7 days).
	// Using 6 days keeps us just inside the cutoff and recovers as much
	// history as the API allows.
	safeStartFromAge = 6 * 24 * time.Hour
)

func newMaliciousPackagePoller(svc malysisv1grpc.MalwareAnalysisServiceClient, cursor *cursorStore) *maliciousPackagePoller {
	return &maliciousPackagePoller{svc: svc, cursor: cursor}
}

// Poll fetches all verified malware records newer than the cursor and calls
// onRecord for each one. The cursor is advanced after each page.
//
// start_from semantics (SafeDep API):
//   - Omitting start_from → server defaults to now-1h (first-run behaviour).
//   - Providing start_from → WHERE created_at > start_from; must be within 7 days.
//   - start_from must stay constant across all pages of one session; only the
//     next_page_token moves forward within the window.
//   - A cursor older than 7 days is rejected with an error; we detect this and
//     reset to safeStartFromAge so the daemon recovers automatically.
func (p *maliciousPackagePoller) Poll(ctx context.Context, onRecord func(*malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error) error {
	lastSeenAt, err := p.cursor.Load(ctx)
	if err != nil {
		return fmt.Errorf("poller: load cursor: %w", err)
	}

	// Guard against a stale cursor. If the daemon was offline for >7 days the
	// API rejects start_from and the daemon would loop on the same error every
	// poll interval. Reset to safeStartFromAge so we recover automatically,
	// accepting that records from the gap period are unrecoverable.
	if !lastSeenAt.IsZero() && lastSeenAt.Before(time.Now().UTC().Add(-apiCutoffAge)) {
		drytui.Warning("Cursor %s exceeds 7-day API cutoff; resetting to %s ago",
			lastSeenAt.UTC().Format(time.RFC3339), safeStartFromAge)
		lastSeenAt = time.Now().UTC().Add(-safeStartFromAge)
		if err := p.cursor.Save(ctx, lastSeenAt); err != nil {
			return fmt.Errorf("poller: save reset cursor: %w", err)
		}
	}

	// Fix start_from for the entire session. The backend uses it as a time
	// filter (WHERE created_at > start_from) and expects it to be constant
	// while next_page_token pages through results within that window.
	// Changing start_from between pages could invalidate the page token.
	sessionStartFrom := lastSeenAt

	filter := &malysisv1.ListPackageAnalysisRecordsRequest_FilterOption{}
	filter.SetOnlyMalware(true)
	filter.SetOnlyVerified(true)

	var pageToken string
	var anySaved bool
	for {
		req := &malysisv1.ListPackageAnalysisRecordsRequest{}
		if !sessionStartFrom.IsZero() {
			req.SetStartFrom(timestamppb.New(sessionStartFrom))
		}
		req.SetFilter(filter)

		pagination := &controltowerv1.PaginationRequest{}
		pagination.SetPageSize(pollPageSize)
		if pageToken != "" {
			pagination.SetPageToken(pageToken)
		}
		req.SetPagination(pagination)

		resp, err := p.svc.ListPackageAnalysisRecords(ctx, req)
		if err != nil {
			return fmt.Errorf("poller: list records: %w", err)
		}

		var pageMaxAt time.Time
		for _, record := range resp.GetRecords() {
			if err := onRecord(record); err != nil {
				return err
			}
			if t := record.GetCreatedAt(); t != nil {
				if ts := t.AsTime(); ts.After(pageMaxAt) {
					pageMaxAt = ts
				}
			}
		}

		// Advance the cursor after processing each page. If records lacked
		// a created_at timestamp, fall back to now so the cursor always moves
		// forward and records are not re-delivered on the next poll cycle.
		if len(resp.GetRecords()) > 0 {
			if pageMaxAt.IsZero() {
				pageMaxAt = time.Now().UTC()
			}
			if err := p.cursor.Save(ctx, pageMaxAt); err != nil {
				return fmt.Errorf("poller: save cursor: %w", err)
			}
			anySaved = true
		}

		nextToken := resp.GetPagination().GetNextPageToken()
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	// Always write the cursor file after a complete poll cycle, even when
	// zero records were returned. This ensures the file exists so operators
	// can edit it to set a past timestamp and re-process history.
	if !anySaved {
		if err := p.cursor.Save(ctx, time.Now().UTC()); err != nil {
			return fmt.Errorf("poller: save cursor: %w", err)
		}
	}

	return nil
}
