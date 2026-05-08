// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"

	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	drytui "github.com/safedep/dry/tui"
)

// feedService bridges a packageSource to the JFrog pusher. The source
// owns delivery cadence and resume state; feedService is concerned only
// with pre-flight validation, the per-record push, and result logging.
type feedService struct {
	source   packageSource
	pusher   *jfrogPusher
	jfrogCfg jfrogConfig
}

func newFeedService(source packageSource, pusher *jfrogPusher, jfrogCfg jfrogConfig) *feedService {
	return &feedService{
		source:   source,
		pusher:   pusher,
		jfrogCfg: jfrogCfg,
	}
}

// Run validates JFrog connectivity once, then hands off to the source.
// Run blocks until ctx is cancelled or the source returns a fatal error.
//
// Pre-flight validation lives here (not in the source) because it is a
// destination-side concern — every source pushes to the same JFrog
// instance, so the check belongs with the pusher's owner.
func (s *feedService) Run(ctx context.Context) error {
	drytui.Info("Validating JFrog connectivity at %s", s.jfrogCfg.url)
	if err := validateJFrog(ctx, s.jfrogCfg); err != nil {
		return err
	}
	drytui.Success("JFrog connectivity OK (URL + token verified)")

	return s.source.Subscribe(ctx, func(record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		return s.handleRecord(ctx, record)
	})
}

// handleRecord pushes a single record to JFrog and emits user-visible
// logs. Push errors are logged and swallowed (best-effort delivery) —
// returning nil keeps the source running for the next record.
//
// The pusher's contract: (status == 0, nil err) means the record was
// skipped before the HTTP call (nil PackageVersion, empty name, or
// empty version). The pusher already logged the reason; we must not
// emit a misleading "Pushed:" line.
func (s *feedService) handleRecord(ctx context.Context, record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
	status, err := s.pusher.Push(ctx, record)
	if err != nil {
		drytui.Warning("Push failed for %s: %v", record.GetAnalysisId(), err)
		return nil
	}
	if status == 0 {
		return nil
	}
	pv := record.GetTarget().GetPackageVersion()
	name := pv.GetPackage().GetName()
	version := pv.GetVersion()
	drytui.Success("Pushed: %s@%s (%s)", name, version, ecosystemToJFrog(pv.GetPackage().GetEcosystem()))
	drytui.Info("  JFrog: %s [%d]", issueID(record.GetAnalysisId()), status)
	return nil
}
