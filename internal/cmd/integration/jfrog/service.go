// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"

	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	drytui "github.com/safedep/dry/tui"
)

// feedService bridges a packageSource to a jfrogClient. The source owns
// delivery cadence and resume state; feedService handles pre-flight
// validation, the per-report push, and operator-visible logging.
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

	return s.source.subscribe(ctx, func(report *threatintelv1.PackageReport) error {
		return s.handleRecord(ctx, report)
	})
}

// handleRecord routes one report to the right JFrog action.
//
// A withdrawn report is a retraction: the package is no longer considered
// malicious. Stage 1 carries it through the pipeline (so the cursor
// advances past it and nothing is lost) but does not act on it yet.
// Stage 2 replaces this branch with a delete of the XRay issue.
func (s *feedService) handleRecord(ctx context.Context, report *threatintelv1.PackageReport) error {
	if report.GetWithdrawn() {
		drytui.Info("Withdrawn report %s (%s): retraction handling not yet enabled, skipping",
			s.client.issueID(report), report.GetPackage().GetName())
		return nil
	}

	return s.handlePush(ctx, report)
}

// handlePush pushes a single report and emits user-visible logs. Push
// errors are logged and swallowed (best-effort delivery): returning nil
// keeps the source running for the next report.
//
// The client's contract: (id="", status==0, nil err) means the report
// was skipped before any HTTP call (no package, empty name, or
// over-length id). The client already logged the reason, so we must not
// emit a misleading "Pushed:" line.
func (s *feedService) handlePush(ctx context.Context, report *threatintelv1.PackageReport) error {
	id, status, err := s.client.pushMaliciousPackage(ctx, report)
	if err != nil {
		drytui.Warning("Push failed for %s: %v", report.GetReportId(), err)
		return nil
	}
	if status == 0 {
		return nil
	}

	name := report.GetPackage().GetName()
	drytui.Success("Pushed: %s (%s)", name, ecosystemToJFrog(report.GetEcosystem()))
	drytui.Info("  JFrog: %s [%d]", id, status)
	return nil
}
