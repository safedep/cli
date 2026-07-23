package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/spinner"
	"github.com/spf13/cobra"
)

const (
	pollInitial = 2 * time.Second
	pollFactor  = 1.5
	pollMax     = 10 * time.Second
)

// runSvc is the union of interfaces run.go needs.
type runSvc interface {
	ScanSubmitter
	ScanGetter
	ScanReportGetter
}

type runInput struct {
	Target  *packagev1.PackageVersion
	Wait    bool
	Timeout time.Duration
	Rescan  bool
	Save    string
}

func runCmd(a *app.App) *cobra.Command {
	var (
		flags   targetFlags
		wait    bool
		timeout time.Duration
		rescan  bool
		save    string
	)
	cmd := &cobra.Command{
		Use:   "run <package-ref>",
		Short: "Submit a package for on-demand scanning",
		Long: "Submit a package version to SafeDep Cloud for on-demand malware scanning and, " +
			"by default, wait for the verdict. Accepts a purl (pkg:npm/lodash@4.17.21), a GitHub " +
			"URL, or the explicit --ecosystem/--name/--version triple.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			target, err := resolveTarget(firstArg(args), flags)
			if err != nil {
				return err
			}

			res, err := runScan(cmd.Context(), NewService(client.Connection()), runInput{
				Target: target, Wait: wait, Timeout: timeout, Rescan: rescan, Save: save,
			}, newSpinnerProgress(a))
			if err != nil {
				return err
			}
			if save != "" && res.report != nil {
				if err := writeReportFile(save, res.report); err != nil {
					return err
				}
				tui.Success("Report written to %s", save)
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&flags.Ecosystem, "ecosystem", "", "package ecosystem (npm, pypi, vscode, openvsx, ...); use with --name/--version")
	f.StringVar(&flags.Name, "name", "", "package name")
	f.StringVar(&flags.Version, "version", "", "package version")
	f.BoolVar(&wait, "wait", true, "wait for the scan to reach a terminal state")
	f.DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for a verdict")
	f.BoolVar(&rescan, "rescan", false, "force a fresh scan instead of reusing an existing one")
	f.StringVar(&save, "save", "", "write the completed report JSON to this path")
	return cmd
}

// progressFn reports scan status transitions to the user. Nil in tests.
type progressFn func(status string)

// runScan submits the target and, when Wait is set, polls until the scan
// reaches a terminal state. The full report is fetched only when it is
// needed: a malware verdict (for inline rendering) or --save.
func runScan(ctx context.Context, svc runSvc, in runInput, onStatus progressFn) (*runResult, error) {
	key := idempotencyKey(in.Target)
	if in.Rescan {
		key = ""
	}
	sub, err := svc.Submit(ctx, SubmitInput{Target: in.Target, IdempotencyKey: key})
	if err != nil {
		return nil, err
	}

	eco, name, version := targetTriple(in.Target)
	headline := Scan{ScanID: sub.ScanID, Ecosystem: eco, Name: name, Version: version, Status: sub.Status}

	if !in.Wait {
		return &runResult{scan: headline, submitted: true}, nil
	}

	scan, err := pollUntilTerminal(ctx, svc, sub.ScanID, in.Timeout, onStatus)
	if err != nil {
		return nil, err
	}

	res := &runResult{scan: *scan}
	if scan.Verdict == verdictMalware || in.Save != "" {
		report, err := svc.GetReport(ctx, scan.ScanID)
		if err != nil {
			return nil, err
		}
		res.report = report
	}
	return res, nil
}

func pollUntilTerminal(ctx context.Context, svc ScanGetter, scanID string, timeout time.Duration, onStatus progressFn) (*Scan, error) {
	deadline := time.Now().Add(timeout)
	backoff := pollInitial
	last := ""
	for {
		scan, err := svc.Get(ctx, scanID)
		if err != nil {
			return nil, err
		}
		if onStatus != nil && scan.Status != last {
			onStatus(scan.Status)
			last = scan.Status
		}
		if isTerminal(scan.Status) {
			return scan, nil
		}
		if time.Now().Add(backoff).After(deadline) {
			return nil, fmt.Errorf("timed out after %s waiting for scan %s (status %s): resume with `safedep package scan get --scan-id %s`",
				timeout, scanID, scan.Status, scanID)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff = time.Duration(float64(backoff) * pollFactor); backoff > pollMax {
			backoff = pollMax
		}
	}
}

func isTerminal(status string) bool {
	return status == "completed" || status == "failed"
}

// newSpinnerProgress returns a progress callback backed by a dry/tui
// spinner in rich mode. The spinner is messaging, so it is independent of
// --output and never appears in plain/json data.
func newSpinnerProgress(a *app.App) progressFn {
	var sp *spinner.Spinner
	return func(status string) {
		if sp == nil {
			sp = spinner.New("scanning: " + status)
			sp.Start()
			return
		}
		if isTerminal(status) {
			sp.Stop("scan " + status)
			return
		}
		sp.Status("scanning: " + status)
	}
}

func writeReportFile(path string, r *Report) error {
	b, err := (&showResult{report: r}).RenderJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

type runResult struct {
	scan      Scan
	report    *Report // populated on malware verdict or --save
	submitted bool    // true when --no-wait: only submitted, not awaited
}

func (r *runResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(scanJSON(r.scan), "", "  ")
}

func (r *runResult) RenderPlain() string {
	return scanPlain(r.scan)
}

func (r *runResult) RenderTable() string {
	if r.submitted {
		return section.Join(
			scanPanel(r.scan, "Package scan submitted", time.Now()),
			section.Hint("Track it: safedep package scan get --scan-id "+r.scan.ScanID),
		)
	}
	now := time.Now()
	panel := scanPanel(r.scan, "Package scan", now)
	// Expand the full report inline for the case people most want detail on.
	if r.report != nil && r.report.IsMalware {
		return section.Join(panel, reportBody(r.report))
	}
	return section.Join(panel,
		section.Hint("Full report: safedep package scan show --scan-id "+r.scan.ScanID))
}
