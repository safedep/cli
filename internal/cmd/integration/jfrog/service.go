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

// feedService orchestrates the poll-and-push loop: it pulls verified malware
// records from SafeDep via the poller and forwards each record to JFrog via
// the pusher. It owns no transport state of its own.
type feedService struct {
	poller   *maliciousPackagePoller
	pusher   *jfrogPusher
	jfrogCfg JFrogConfig
	poll     time.Duration
}

func newFeedService(svc malysisv1grpc.MalwareAnalysisServiceClient, cfg Config) *feedService {
	cursor := newCursorStore(cfg.Source.CursorFile)
	return &feedService{
		poller:   newMaliciousPackagePoller(svc, cursor),
		pusher:   newJFrogPusher(cfg.JFrog),
		jfrogCfg: cfg.JFrog,
		poll:     cfg.Source.PollInterval,
	}
}

// Run executes poll cycles until ctx is cancelled (SIGINT / SIGTERM).
//
// A pre-flight connectivity check runs once before the loop starts so
// misconfigured URL or token fail fast at startup with a clear message,
// rather than after the first poll cycle deep inside the push path.
//
// A cycle that fails mid-flight (poller error) is logged and the loop
// continues — transient gRPC failures must not bring down the daemon.
// Cancellation between cycles is honoured immediately.
func (s *feedService) Run(ctx context.Context) error {
	drytui.Info("Validating JFrog connectivity at %s", s.jfrogCfg.URL)
	if err := validateJFrog(ctx, s.jfrogCfg); err != nil {
		return err
	}
	drytui.Success("JFrog connectivity OK (URL + token verified)")

	drytui.Info("Starting JFrog integration feed (poll interval: %s)", s.poll)

	for {
		if err := s.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			drytui.Warning("Poll cycle error: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.poll):
		}
	}
}

// runOnce performs a single poll-and-push cycle.
//
// Push failures are logged and swallowed (best-effort delivery). The cursor
// still advances after the page so a single bad record cannot block the
// whole stream forever. JFrog issue IDs are deterministic, so a record that
// later succeeds to push will overwrite the partial state safely.
func (s *feedService) runOnce(ctx context.Context) error {
	var pushed int
	err := s.poller.Poll(ctx, func(record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		status, err := s.pusher.Push(ctx, record)
		if err != nil {
			log.Warnf("feed: push %s: %v", record.GetAnalysisId(), err)
			return nil
		}
		// Push contract: status == 0 with nil error means the record was
		// skipped before the HTTP call (nil PackageVersion, empty name, or
		// empty version). The pusher already logged the reason; we must not
		// count it as pushed or emit a misleading "Pushed:" line.
		if status == 0 {
			return nil
		}
		pushed++
		pv := record.GetTarget().GetPackageVersion()
		name := pv.GetPackage().GetName()
		version := pv.GetVersion()
		drytui.Info("Pushed: %s@%s (%s)", name, version, ecosystemToJFrog(pv.GetPackage().GetEcosystem()))
		drytui.Info("  JFrog: %s [%d]", issueID(name, version), status)
		return nil
	})
	if err != nil {
		return err
	}
	drytui.Info("Feed cycle complete: pushed %d records", pushed)
	return nil
}
