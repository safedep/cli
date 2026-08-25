// internal/cmd/integration/jfrog/service.go
package jfrog

import (
	"context"
	"net/http"
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

// handleRecord routes one report: a withdrawn report deletes its XRay issue, any
// other malicious report is pushed.
func (s *feedService) handleRecord(ctx context.Context, report *threatintelv1.PackageReport) error {
	if report.GetWithdrawn() {
		return s.handleDelete(ctx, report)
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

// handleDelete removes the XRay issue for a withdrawn report best-effort:
// errors are logged, never fatal. A status of 0 means the client skipped it
// (never pushed, or the print client already logged its preview), so stay quiet.
// A 404 is benign: the issue is already absent.
func (s *feedService) handleDelete(ctx context.Context, report *threatintelv1.PackageReport) error {
	id, status, err := s.client.deleteMaliciousPackage(ctx, report)
	if err != nil {
		drytui.Warning("Delete failed for %s: %v", report.GetReportId(), err)
		return nil
	}
	if status == 0 {
		return nil
	}

	name := report.GetPackage().GetName()
	if status == http.StatusNotFound {
		drytui.Info("Withdrawn %s (%s): issue %s already absent in XRay", report.GetReportId(), name, id)
		return nil
	}

	drytui.Success("Deleted: %s (%s)", name, ecosystemToJFrog(report.GetEcosystem()))
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
