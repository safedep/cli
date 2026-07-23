package packages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

type listInput struct {
	Flags     targetFlags
	PageSize  uint32
	PageToken string
}

func listCmd(a *app.App) *cobra.Command {
	var (
		flags     targetFlags
		pageSize  uint32
		pageToken string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List package scans",
		Long: "List package scans for the active tenant, newest first. Optionally filter to one " +
			"package version with --ecosystem/--name/--version.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runList(cmd.Context(), NewService(client.Connection()), listInput{
				Flags: flags, PageSize: pageSize, PageToken: pageToken,
			})
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&flags.Ecosystem, "ecosystem", "", "filter: package ecosystem (requires --name and --version)")
	f.StringVar(&flags.Name, "name", "", "filter: package name")
	f.StringVar(&flags.Version, "version", "", "filter: package version")
	f.Uint32Var(&pageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&pageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func runList(ctx context.Context, lister ScanLister, in listInput) (*listResult, error) {
	svcIn := ListInput{PageSize: in.PageSize, PageToken: in.PageToken}
	if in.Flags.any() {
		// The API filter is an exact package version, so the full triple is required.
		target, err := resolveExplicit(in.Flags)
		if err != nil {
			return nil, err
		}
		svcIn.Target = target
	}
	res, err := lister.List(ctx, svcIn)
	if err != nil {
		return nil, err
	}
	return &listResult{scans: res.Scans, nextPage: res.NextPage}, nil
}

type listResult struct {
	scans    []Scan
	nextPage string
}

func (r *listResult) RenderJSON() ([]byte, error) {
	out := struct {
		Scans    []scanJSONObj `json:"scans"`
		NextPage string        `json:"next_page_token,omitempty"`
	}{NextPage: r.nextPage}
	for _, s := range r.scans {
		out.Scans = append(out.Scans, scanJSON(s))
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *listResult) RenderPlain() string {
	if len(r.scans) == 0 {
		return "no scans"
	}
	var b strings.Builder
	for _, s := range r.scans {
		b.WriteString(scanPlain(s))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *listResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.scans))
	for _, s := range r.scans {
		rows = append(rows, []string{
			s.ScanID, s.Ecosystem, s.Name, s.Version, s.Status, verdictBadge(s.Verdict), humanize.Time(s.CreatedAt, now),
		})
	}
	t := table.New().
		Title("Package scans").
		Headers("Scan ID", "Ecosystem", "Name", "Version", "Status", "Verdict", "Created").
		Rows(rows...).
		EmptyMessage("No scans found. Submit one with: safedep package scan run <package-ref>")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), plural(len(rows), "scan", "scans"))
		if r.nextPage != "" {
			footer += fmt.Sprintf(". More available: --page-token %s", r.nextPage)
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
