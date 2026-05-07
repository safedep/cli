package jfrog

import (
	"context"
	"errors"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	"github.com/safedep/cli/internal/storage"
	drytui "github.com/safedep/dry/tui"
)

// pollSource is a packageSource backed by SafeDep's gRPC pull API.
// It runs an infinite poll loop — sleeping pollInterval between cycles
// — and uses a profile-scoped KV cursor (see store.go) to resume across
// restarts.
//
// All cursor, pagination, and 7-day-cutoff handling is encapsulated
// here and in poller.go + store.go. A future streamSource would not
// share any of this state — they are entirely independent.
type pollSource struct {
	poller       *maliciousPackagePoller
	pollInterval time.Duration
}

func newPollSource(svc malysisv1grpc.MalwareAnalysisServiceClient, kv *storage.KV[time.Time], pollInterval time.Duration) *pollSource {
	return &pollSource{
		poller:       newMaliciousPackagePoller(svc, newCursorStore(kv)),
		pollInterval: pollInterval,
	}
}

// Subscribe drives the poll loop until ctx is cancelled.
//
// Per-cycle errors (gRPC failures, transient network) are surfaced via
// drytui.Warning and the loop continues — a single bad cycle must not
// bring down the daemon. Cancellation between cycles is honoured
// immediately.
func (s *pollSource) Subscribe(ctx context.Context, onRecord recordHandler) error {
	drytui.Info("Starting JFrog feed poller (interval: %s)", s.pollInterval)

	for {
		err := s.poller.Poll(ctx, onRecord)
		switch {
		case err == nil:
			drytui.Info("Poll cycle complete, next in %s", s.pollInterval)
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
			drytui.Warning("Poll cycle error: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.pollInterval):
		}
	}
}
