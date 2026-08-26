package jfrog

import (
	"context"

	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	drytui "github.com/safedep/dry/tui"
)

var _ xrayClient = (*printClient)(nil)

// printClient is the dry-run adapter. It builds the event the real client would
// push, with the shared buildEvent, then previews it instead of sending. The
// preview is a user-facing result, so it goes through the reporter. The dry-run
// banner and any skip are operational logs.
type printClient struct {
	rep *reporter
}

func newPrintClient(rep *reporter) *printClient { return &printClient{rep: rep} }

func (c *printClient) validate(_ context.Context) error {
	c.rep.logInfo("Dry run: previewing the feed, nothing is sent to JFrog (no JFrog credentials needed)")
	return nil
}

// pushMaliciousPackage previews the push and returns status 0 so the service's
// handlePush stays quiet (this method already reported the preview). A skipped
// report returns ("", 0, nil), matching the real client.
func (c *printClient) pushMaliciousPackage(_ context.Context, report *threatintelv1.PackageReport) (string, int, error) {
	event, reason, ok := buildEvent(report)
	if !ok {
		logSkip(c.rep, report, reason)
		return "", 0, nil
	}

	name := report.GetPackage().GetName()
	c.rep.result(
		func() {
			drytui.Success("Would push: %s (%s) versions: %s", name, event.PackageType, displayVersions(report.GetPackage().GetVersions()))
			drytui.Info("  JFrog issue id: %s", event.ID)
		},
		jsonEvent{
			Event:     eventDryRunPush,
			ReportID:  report.GetReportId(),
			Package:   name,
			Ecosystem: event.PackageType,
			Versions:  cleanVersions(report.GetPackage().GetVersions()),
			IssueID:   event.ID,
		})
	return event.ID, 0, nil
}

// deleteMaliciousPackage previews the delete and sends nothing. It returns
// status 0 so the service stays quiet. A skipped id returns ("", 0, nil).
func (c *printClient) deleteMaliciousPackage(_ context.Context, report *threatintelv1.PackageReport) (string, int, error) {
	id := issueID(report)
	if len(id) > maxIssueIDLen {
		return "", 0, nil
	}

	name := report.GetPackage().GetName()
	eco := ecosystemToJFrog(report.GetEcosystem())
	c.rep.result(
		func() {
			drytui.Success("Would delete: %s (%s)", name, eco)
			drytui.Info("  JFrog issue id: %s", id)
		},
		jsonEvent{
			Event:     eventDryRunDelete,
			ReportID:  report.GetReportId(),
			Package:   name,
			Ecosystem: eco,
			IssueID:   id,
		})
	return id, 0, nil
}
