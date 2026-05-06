package jfrog

import (
	"context"
	"fmt"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// maliciousPackagePoller drives one poll cycle against SafeDep's malware
// analysis API. It is stateless except for the file-backed cursor — the
// daemon loop in feedService is responsible for invoking it on a schedule.
type maliciousPackagePoller struct {
	svc    malysisv1grpc.MalwareAnalysisServiceClient
	cursor *cursorStore
}

// pollPageSize is the number of records requested per page. Tuned for a
// balance between API roundtrips and memory: at 100 a single page is small
// enough to keep memory bounded and large enough to make pagination cheap.
const pollPageSize = 100

func newMaliciousPackagePoller(svc malysisv1grpc.MalwareAnalysisServiceClient, cursor *cursorStore) *maliciousPackagePoller {
	return &maliciousPackagePoller{svc: svc, cursor: cursor}
}

// Poll fetches all verified malware records newer than the cursor and calls
// onRecord for each one. The cursor is advanced after each page.
func (p *maliciousPackagePoller) Poll(ctx context.Context, onRecord func(*malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error) error {
	lastSeenAt, err := p.cursor.Load()
	if err != nil {
		return fmt.Errorf("poller: load cursor: %w", err)
	}

	filter := &malysisv1.ListPackageAnalysisRecordsRequest_FilterOption{}
	filter.SetOnlyMalware(true)
	filter.SetOnlyVerified(true)

	var pageToken string
	var anySaved bool
	for {
		req := &malysisv1.ListPackageAnalysisRecordsRequest{}
		if !lastSeenAt.IsZero() {
			req.SetStartFrom(timestamppb.New(lastSeenAt))
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
			if err := p.cursor.Save(pageMaxAt); err != nil {
				return fmt.Errorf("poller: save cursor: %w", err)
			}
			lastSeenAt = pageMaxAt
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
		if err := p.cursor.Save(time.Now().UTC()); err != nil {
			return fmt.Errorf("poller: save cursor: %w", err)
		}
	}

	return nil
}
