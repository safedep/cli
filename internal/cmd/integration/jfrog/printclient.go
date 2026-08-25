package jfrog

import (
	"context"

	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	drytui "github.com/safedep/dry/tui"
)

// xrayClient is the port the feed service pushes through. jfrogClient is the
// real adapter (HTTP to XRay); printClient is the dry-run adapter that prints
// what would be pushed and sends nothing. Swapping the adapter is the only
// difference between a real run and a dry-run.
type xrayClient interface {
	validate(ctx context.Context) error
	pushMaliciousPackage(ctx context.Context, report *threatintelv1.PackageReport) (string, int, error)
}

var (
	_ xrayClient = (*jfrogClient)(nil)
	_ xrayClient = (*printClient)(nil)
)

// printClient is the dry-run adapter. It builds the event the real client would
// push, via the shared buildEvent, then prints it instead of sending. It holds
// no state and never opens a connection, so it needs no JFrog credentials.
type printClient struct{}

func newPrintClient() *printClient { return &printClient{} }

func (c *printClient) validate(_ context.Context) error {
	drytui.Info("Dry run: previewing the feed, nothing is sent to JFrog (no JFrog credentials needed)")
	return nil
}

// pushMaliciousPackage builds the event the real push would send, prints it, and
// returns status 0 so the service's handlePush stays quiet (this method already
// logged the preview line). A skipped report returns ("", 0, nil), matching the
// real client.
func (c *printClient) pushMaliciousPackage(_ context.Context, report *threatintelv1.PackageReport) (string, int, error) {
	event, ok := buildEvent(report)
	if !ok {
		// buildEvent already logged why it is skipped.
		return "", 0, nil
	}

	versions := displayVersions(report.GetPackage().GetVersions())
	drytui.Success("Would push: %s (%s) versions: %s", report.GetPackage().GetName(), event.PackageType, versions)
	drytui.Info("  JFrog issue id: %s", event.ID)
	drytui.Info("  summary: %s", event.Summary)
	return event.ID, 0, nil
}
