package packages

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

// getSvc is the union of interfaces get.go needs: direct fetch by id, and
// newest-scan resolution by target.
type getSvc interface {
	ScanGetter
	ScanLister
}

type getInput struct {
	ScanID string
	Flags  targetFlags
	Ref    string
}

func getCmd(a *app.App) *cobra.Command {
	var (
		scanID string
		flags  targetFlags
	)
	cmd := &cobra.Command{
		Use:   "get <package-ref>",
		Short: "Get the status and verdict of a scan",
		Long: "Get the status and verdict of a package scan without the full report. Addresses " +
			"the scan by package-ref (the newest scan for that package) or directly by --scan-id.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			scan, err := runGet(cmd.Context(), NewService(client.Connection()), getInput{
				ScanID: scanID, Flags: flags, Ref: firstArg(args),
			})
			if err != nil {
				return err
			}
			return a.Output.Print(&scanResult{scan: *scan})
		},
	}
	f := cmd.Flags()
	f.StringVar(&scanID, "scan-id", "", "address the scan directly by id (skips target resolution)")
	f.StringVar(&flags.Ecosystem, "ecosystem", "", "package ecosystem; use with --name/--version")
	f.StringVar(&flags.Name, "name", "", "package name")
	f.StringVar(&flags.Version, "version", "", "package version")
	return cmd
}

func runGet(ctx context.Context, svc getSvc, in getInput) (*Scan, error) {
	if in.ScanID != "" {
		return svc.Get(ctx, in.ScanID)
	}
	target, err := resolveTarget(in.Ref, in.Flags)
	if err != nil {
		return nil, err
	}
	return latestScan(ctx, svc, target)
}

// scanResult renders a single Scan headline (get, and the reused shape).
type scanResult struct{ scan Scan }

func (r *scanResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(scanJSON(r.scan), "", "  ")
}

func (r *scanResult) RenderPlain() string { return scanPlain(r.scan) }

func (r *scanResult) RenderTable() string {
	return scanPanel(r.scan, "Package scan", time.Now())
}

// Shared JSON/plain projections used by get and run.

type targetJSON struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

type scanJSONObj struct {
	ScanID      string     `json:"scan_id"`
	Target      targetJSON `json:"target"`
	Status      string     `json:"status"`
	Verdict     string     `json:"verdict,omitempty"`
	Confidence  float64    `json:"confidence"`
	Failure     string     `json:"failure_reason,omitempty"`
	CreatedAt   string     `json:"created_at,omitempty"`
	CompletedAt string     `json:"completed_at,omitempty"`
}

func scanJSON(s Scan) scanJSONObj {
	o := scanJSONObj{
		ScanID:     s.ScanID,
		Target:     targetJSON{Ecosystem: s.Ecosystem, Name: s.Name, Version: s.Version},
		Status:     s.Status,
		Verdict:    s.Verdict,
		Confidence: s.Confidence,
		Failure:    s.Failure,
	}
	if !s.CreatedAt.IsZero() {
		o.CreatedAt = s.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !s.CompletedAt.IsZero() {
		o.CompletedAt = s.CompletedAt.UTC().Format(time.RFC3339)
	}
	return o
}

func scanPlain(s Scan) string {
	return strings.Join([]string{
		s.Ecosystem, s.Name, s.Version, s.Status, dash(s.Verdict), confidencePct(s.Confidence), s.ScanID,
	}, "\t")
}
