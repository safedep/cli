package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/panel"
	"github.com/safedep/dry/tui/section"
	"github.com/spf13/cobra"
)

// showSvc is the union of interfaces show.go needs.
type showSvc interface {
	ScanReportGetter
	ScanLister
}

type showInput struct {
	ScanID string
	Flags  targetFlags
	Ref    string
	Save   string
}

func showCmd(a *app.App) *cobra.Command {
	var (
		scanID string
		flags  targetFlags
		save   string
	)
	cmd := &cobra.Command{
		Use:   "show <package-ref>",
		Short: "Show the full report of a completed scan",
		Long: "Show the full analysis report of a completed package scan. Addresses the scan by " +
			"package-ref (the newest scan for that package) or directly by --scan-id.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runShow(cmd.Context(), NewService(client.Connection()), showInput{
				ScanID: scanID, Flags: flags, Ref: firstArg(args), Save: save,
			})
			if err != nil {
				return err
			}
			if save != "" {
				if err := writeReportFile(save, res.report); err != nil {
					return err
				}
				tui.Success("Report written to %s", save)
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&scanID, "scan-id", "", "address the scan directly by id (skips target resolution)")
	f.StringVar(&flags.Ecosystem, "ecosystem", "", "package ecosystem; use with --name/--version")
	f.StringVar(&flags.Name, "name", "", "package name")
	f.StringVar(&flags.Version, "version", "", "package version")
	f.StringVar(&save, "save", "", "write the report JSON to this path")
	return cmd
}

func runShow(ctx context.Context, svc showSvc, in showInput) (*showResult, error) {
	scanID := in.ScanID
	if scanID == "" {
		target, err := resolveTarget(in.Ref, in.Flags)
		if err != nil {
			return nil, err
		}
		latest, err := latestScan(ctx, svc, target)
		if err != nil {
			return nil, err
		}
		scanID = latest.ScanID
	}
	report, err := svc.GetReport(ctx, scanID)
	if err != nil {
		return nil, err
	}
	if report.Status != "completed" {
		return nil, fmt.Errorf("scan %s is %s, no report yet: poll with `safedep package scan get --scan-id %s`",
			scanID, report.Status, scanID)
	}
	return &showResult{report: report}, nil
}

type showResult struct{ report *Report }

func (r *showResult) RenderTable() string {
	now := time.Now()
	header := panel.New("Package scan report").
		Field("Package", packageLabel(r.report.Scan)).
		Field("Verdict", verdictBadge(r.report.Verdict)).
		Field("Confidence", confidencePct(r.report.Confidence)).
		FieldIf(!r.report.AnalyzedAt.IsZero(), "Analyzed", humanTime(r.report.AnalyzedAt, now)).
		FieldIf(r.report.ReportID != "", "Report ID", r.report.ReportID).
		Render()

	body := reportBody(r.report)
	if body == "" {
		return section.Join(header, section.Empty("No evidence or warnings recorded."))
	}
	return section.Join(header, body)
}

func (r *showResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString(scanPlain(r.report.Scan))
	b.WriteString("\n")
	if r.report.Summary != "" {
		fmt.Fprintf(&b, "summary\t%s\n", r.report.Summary)
	}
	for _, e := range r.report.FileEvidences {
		fmt.Fprintf(&b, "file\t%s\t%d\t%s\t%s\n", e.File, e.Line, e.Title, e.Confidence)
	}
	for _, e := range r.report.ProjectEvidences {
		fmt.Fprintf(&b, "project\t%s\t%s\t%s\n", e.Project, e.Title, e.Confidence)
	}
	for _, w := range r.report.Warnings {
		fmt.Fprintf(&b, "warning\t%s\n", w)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *showResult) RenderJSON() ([]byte, error) {
	type fileEvJSON struct {
		File       string `json:"file"`
		Line       int32  `json:"line,omitempty"`
		Title      string `json:"title,omitempty"`
		Behavior   string `json:"behavior,omitempty"`
		Details    string `json:"details,omitempty"`
		Confidence string `json:"confidence,omitempty"`
	}
	type projEvJSON struct {
		Project    string `json:"project,omitempty"`
		URL        string `json:"url,omitempty"`
		Title      string `json:"title,omitempty"`
		Behavior   string `json:"behavior,omitempty"`
		Details    string `json:"details,omitempty"`
		Confidence string `json:"confidence,omitempty"`
	}
	out := struct {
		Scan             scanJSONObj  `json:"scan"`
		ReportID         string       `json:"report_id,omitempty"`
		AnalyzedAt       string       `json:"analyzed_at,omitempty"`
		IsMalware        bool         `json:"is_malware"`
		Summary          string       `json:"summary,omitempty"`
		Details          string       `json:"details,omitempty"`
		FileEvidences    []fileEvJSON `json:"file_evidences,omitempty"`
		ProjectEvidences []projEvJSON `json:"project_evidences,omitempty"`
		Warnings         []string     `json:"warnings,omitempty"`
	}{
		Scan:      scanJSON(r.report.Scan),
		ReportID:  r.report.ReportID,
		IsMalware: r.report.IsMalware,
		Summary:   r.report.Summary,
		Details:   r.report.Details,
		Warnings:  r.report.Warnings,
	}
	if !r.report.AnalyzedAt.IsZero() {
		out.AnalyzedAt = r.report.AnalyzedAt.UTC().Format(time.RFC3339)
	}
	for _, e := range r.report.FileEvidences {
		out.FileEvidences = append(out.FileEvidences, fileEvJSON(e))
	}
	for _, e := range r.report.ProjectEvidences {
		out.ProjectEvidences = append(out.ProjectEvidences, projEvJSON(e))
	}
	return json.MarshalIndent(out, "", "  ")
}
