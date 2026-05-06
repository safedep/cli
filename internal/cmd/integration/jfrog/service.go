// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/safedep/dry/log"
	drytui "github.com/safedep/dry/tui"
)

type feedService struct {
	poller *maliciousPackagePoller
	pusher *jfrogPusher
	poll   time.Duration
}

func newFeedService(svc malysisv1grpc.MalwareAnalysisServiceClient, cfg Config) *feedService {
	cursor := newCursorStore(cfg.Source.CursorFile)
	return &feedService{
		poller: newMaliciousPackagePoller(svc, cursor),
		pusher: newJFrogPusher(cfg.JFrog),
		poll:   cfg.Source.PollInterval,
	}
}

// Run polls SafeDep for verified malware and pushes to JFrog until ctx is cancelled.
func (s *feedService) Run(ctx context.Context) error {
	drytui.Info("Starting JFrog integration feed (poll interval: %s)", s.poll)

	for {
		if err := s.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warnf("feed: poll cycle error: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.poll):
		}
	}
}

func (s *feedService) runOnce(ctx context.Context) error {
	var pushed int
	err := s.poller.Poll(ctx, func(record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		if err := s.pusher.Push(ctx, record); err != nil {
			log.Warnf("feed: push %s: %v", record.GetAnalysisId(), err)
			return nil
		}
		pushed++
		pv := record.GetTarget().GetPackageVersion()
		drytui.Info("Pushed: %s@%s (%s) → %s",
			pv.GetPackage().GetName(),
			pv.GetVersion(),
			pv.GetPackage().GetEcosystem(),
			issueID(pv.GetPackage().GetName(), pv.GetVersion()),
		)
		return nil
	})
	if err != nil {
		return err
	}
	drytui.Info("Feed cycle complete: pushed %d records", pushed)
	return nil
}
