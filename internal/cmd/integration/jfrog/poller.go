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

type maliciousPackagePoller struct {
	svc    malysisv1grpc.MalwareAnalysisServiceClient
	cursor *cursorStore
}

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
	for {
		req := &malysisv1.ListPackageAnalysisRecordsRequest{}
		if !lastSeenAt.IsZero() {
			req.SetStartFrom(timestamppb.New(lastSeenAt))
		}
		req.SetFilter(filter)

		pagination := &controltowerv1.PaginationRequest{}
		pagination.SetPageSize(100)
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

		if !pageMaxAt.IsZero() {
			if err := p.cursor.Save(pageMaxAt); err != nil {
				return fmt.Errorf("poller: save cursor: %w", err)
			}
			lastSeenAt = pageMaxAt
		}

		nextToken := resp.GetPagination().GetNextPageToken()
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	return nil
}
