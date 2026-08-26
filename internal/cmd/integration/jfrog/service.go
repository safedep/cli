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
	rep    *reporter
}

func newFeedService(source packageSource, client xrayClient, rep *reporter) *feedService {
	return &feedService{source: source, client: client, rep: rep}
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
		// An error is operational, so it is a log, not output.
		s.rep.logWarn("Push failed for %s: %v", report.GetReportId(), err)
		return nil
	}
	if status == 0 {
		return nil
	}

	name := report.GetPackage().GetName()
	eco := ecosystemToJFrog(report.GetEcosystem())
	if status == http.StatusBadRequest {
		// Already pushed to XRay (see pushMaliciousPackage). Nothing changed, so
		// this is a log, not output. It is frequent, so it is dimmed.
		s.rep.logDim("Already pushed %s (%s): issue %s", report.GetReportId(), name, id)
		return nil
	}

	// A real state change (package blocked in XRay). This is user-facing output.
	versions := report.GetPackage().GetVersions()
	s.rep.result(
		func() {
			drytui.Success("Pushed: %s (%s) versions: %s", name, eco, displayVersions(versions))
			drytui.Info("  JFrog: %s [%d]", id, status)
		},
		jsonEvent{Event: eventPushed, ReportID: report.GetReportId(), Package: name, Ecosystem: eco, Versions: cleanVersions(versions), IssueID: id, Status: status})
	return nil
}

// handleDelete removes the XRay issue for a withdrawn report best-effort:
// errors are logged, never fatal. A status of 0 means the client skipped it
// (never pushed, or the print client already logged its preview), so stay quiet.
// A 404 is benign: the issue is already absent.
func (s *feedService) handleDelete(ctx context.Context, report *threatintelv1.PackageReport) error {
	id, status, err := s.client.deleteMaliciousPackage(ctx, report)
	if err != nil {
		// An error is operational, so it is a log, not output.
		s.rep.logWarn("Delete failed for %s: %v", report.GetReportId(), err)
		return nil
	}
	if status == 0 {
		return nil
	}

	name := report.GetPackage().GetName()
	eco := ecosystemToJFrog(report.GetEcosystem())
	if status == http.StatusNotFound {
		// The issue does not exist in XRay. This is the state the delete wants,
		// and nothing changed, so it is a log, not output. It is frequent, so it
		// is dimmed.
		s.rep.logDim("Package to delete, does not exist %s (%s): issue %s", report.GetReportId(), name, id)
		return nil
	}

	// A real state change (block removed in XRay). This is user-facing output.
	s.rep.result(
		func() {
			drytui.Success("Deleted: %s (%s)", name, eco)
			drytui.Info("  JFrog: %s [%d]", id, status)
		},
		jsonEvent{Event: eventDeleted, ReportID: report.GetReportId(), Package: name, Ecosystem: eco, IssueID: id, Status: status})
	return nil
}

// cleanVersions drops empty entries from the affected-version list. An empty
// result means all versions, mirroring vulnerableVersionRanges.
func cleanVersions(versions []string) []string {
	cleaned := make([]string, 0, len(versions))
	for _, v := range versions {
		if v != "" {
			cleaned = append(cleaned, v)
		}
	}
	return cleaned
}

// displayVersions renders affected versions for the human log line. Empty means
// all versions.
func displayVersions(versions []string) string {
	cleaned := cleanVersions(versions)
	if len(cleaned) == 0 {
		return "all"
	}
	return strings.Join(cleaned, ", ")
}
