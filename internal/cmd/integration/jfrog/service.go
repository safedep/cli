// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"
	"strings"

	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	drytui "github.com/safedep/dry/tui"
)

// feedService bridges a packageSource to an xrayClient: validate once, then
// route each report the source delivers. The client is a port (client.go), so
// a real push and a dry-run preview share this file unchanged, differing only
// in which xrayClient is wired in.
type feedService struct {
	source packageSource
	client xrayClient
}

func newFeedService(source packageSource, client xrayClient) *feedService {
	return &feedService{source: source, client: client}
}

// run validates the client once, then blocks in the source until ctx is
// cancelled. Validation is a destination concern, so it lives here, not in the
// source. Each xrayClient owns its own validate messaging.
func (s *feedService) run(ctx context.Context) error {
	if err := s.client.validate(ctx); err != nil {
		return err
	}

	return s.source.subscribe(ctx, func(report *threatintelv1.PackageReport) error {
		return s.handleRecord(ctx, report)
	})
}

// handleRecord routes one report. Stage 1 carries withdrawals through (so the
// cursor advances) but does not act on them; Stage 2 will delete the issue here.
func (s *feedService) handleRecord(ctx context.Context, report *threatintelv1.PackageReport) error {
	if report.GetWithdrawn() {
		drytui.Info("Withdrawn report %s (%s): retraction handling not yet enabled, skipping",
			issueID(report), report.GetPackage().GetName())
		return nil
	}

	return s.handlePush(ctx, report)
}

// handlePush pushes a report best-effort: errors are logged, never fatal. A
// status of 0 means the client skipped it (and already logged why), so stay
// quiet rather than print a misleading "Pushed:" line. The print client also
// returns 0, having already logged its own "Would push:" line.
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
	versions := displayVersions(report.GetPackage().GetVersions())
	drytui.Success("Pushed: %s (%s) versions: %s", name, ecosystemToJFrog(report.GetEcosystem()), versions)
	drytui.Info("  JFrog: %s [%d]", id, status)
	return nil
}

// displayVersions renders affected versions for the log line. Empty means all
// versions, mirroring vulnerableVersionRanges.
func displayVersions(versions []string) string {
	cleaned := make([]string, 0, len(versions))
	for _, v := range versions {
		if v != "" {
			cleaned = append(cleaned, v)
		}
	}
	if len(cleaned) == 0 {
		return "all"
	}
	return strings.Join(cleaned, ", ")
}
