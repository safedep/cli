// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"

	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	drytui "github.com/safedep/dry/tui"
)

// feedService bridges a packageSource to a jfrogClient. The source owns
// delivery cadence and resume state; feedService handles pre-flight
// validation, the per-record push, and operator-visible logging.
type feedService struct {
	source packageSource
	client *jfrogClient
}

func newFeedService(source packageSource, client *jfrogClient) *feedService {
	return &feedService{source: source, client: client}
}

// Run validates JFrog connectivity once, then hands off to the source.
// Run blocks until ctx is cancelled or the source returns a fatal error.
//
// Pre-flight validation lives here (not in the source) because it is a
// destination-side concern: every source pushes to the same JFrog
// instance, so the check belongs with the client's owner.
func (s *feedService) run(ctx context.Context) error {
	drytui.Info("Validating JFrog connectivity")
	if err := s.client.validate(ctx); err != nil {
		return err
	}
	drytui.Success("JFrog connectivity OK (URL + token verified)")

	return s.source.subscribe(ctx, func(record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		return s.handleRecord(ctx, record)
	})
}

// handleRecord pushes a single record and emits user-visible logs.
// Push errors are logged and swallowed (best-effort delivery): returning
// nil keeps the source running for the next record.
//
// The client's contract: (id="", status==0, nil err) means the record
// was skipped before any HTTP call (nil PackageVersion, empty name, or
// empty version). The client already logged the reason, so we must not
// emit a misleading "Pushed:" line.
func (s *feedService) handleRecord(ctx context.Context, record *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
	id, status, err := s.client.pushMaliciousPackage(ctx, record)
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
	drytui.Info("  JFrog: %s [%d]", id, status)
	return nil
}
