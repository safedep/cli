package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

// This file holds `project list`, the list of the projects themselves.
// `project scan list` lists scans and lives in list.go.

const projectSourcePrefix = "SOURCE_"

type projectListInput struct {
	Selector  string
	Sources   []string
	Names     []string
	PageSize  uint32
	PageToken string
}

type projectListResult struct {
	Projects      []projectView `json:"projects"`
	NextPageToken string        `json:"next_page_token,omitempty"`
}

func projectListCmd(a *app.App) *cobra.Command {
	var in projectListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SafeDep projects with their labels",
		Long: "List the projects of the tenant with their labels. --selector takes a label selector, as a " +
			"policy override does, so you can see which projects an override selects: " +
			"key=value, key!=value, key in (a,b), key notin (a,b), key or !key, separated by commas. " +
			"The terms AND together. The command reads one page. Pass --page-token to read " +
			"the next page.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			_, err := newProjectListRequest(in)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runProjectList(cmd.Context(), newListProjectsClient(client.Connection()), in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Selector, "selector", "", "filter: a label selector, as in custom/criticality=high,scm/forge=github")
	f.StringArrayVar(&in.Sources, "source", nil,
		"filter: project source ("+strings.Join(projectSourceTokens(), ", ")+"); repeatable")
	f.StringArrayVar(&in.Names, "name", nil,
		fmt.Sprintf("filter: exact project name; repeat for up to %d names", maxScanFilterValues))
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func projectSourceTokens() []string {
	return tui.EnumTokens(messagescontroltowerv1.Project_Source_name, projectSourcePrefix)
}

func newProjectListRequest(in projectListInput) (*controltowerv1.ListProjectsRequest, error) {
	filter := &controltowerv1.ListProjectsRequest_Filter{}
	set := false
	if in.Selector != "" {
		selector, err := cloudpolicy.ParseSelector(in.Selector)
		if err != nil {
			return nil, invalidProjectSelectionError(err, "Write the selector as key=value, as in custom/team=release")
		}
		filter.SetLabelSelector(selector)
		set = true
	}
	if len(in.Sources) > 0 {
		sources := make([]messagescontroltowerv1.Project_Source, 0, len(in.Sources))
		for _, token := range in.Sources {
			n, ok := tui.ParseEnumToken(messagescontroltowerv1.Project_Source_name, projectSourcePrefix, token)
			if !ok {
				return nil, unknownFilterValueError("source", token, projectSourceTokens())
			}
			sources = append(sources, messagescontroltowerv1.Project_Source(n))
		}
		filter.SetSources(sources)
		set = true
	}
	if len(in.Names) > 0 {
		if err := validateScanFilterValues(in.Names, "project name"); err != nil {
			return nil, err
		}
		filter.SetProjectNames(in.Names)
		set = true
	}
	req := &controltowerv1.ListProjectsRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if set {
		req.SetFilterV2(filter)
	}
	return req, nil
}

func runProjectList(ctx context.Context, client listProjectsClient, in projectListInput) (*projectListResult, error) {
	req, err := newProjectListRequest(in)
	if err != nil {
		return nil, err
	}
	res, err := client.ListProjects(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("project list: %w", err)
	}
	out := &projectListResult{
		Projects:      make([]projectView, 0, len(res.GetProjects())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, p := range res.GetProjects() {
		out.Projects = append(out.Projects, newProjectView(p))
	}
	return out, nil
}

func (r *projectListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *projectListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Projects))
	for _, p := range r.Projects {
		created := "-"
		if p.created != nil {
			created = humanize.Time(*p.created, now)
		}
		labels := strings.Join(p.Labels, ", ")
		if labels == "" {
			labels = "-"
		}
		rows = append(rows, []string{
			p.Name, p.Source, labels, countCell(p.Vulnerabilities), countCell(p.PolicyViolations), created,
		})
	}
	t := table.New().
		Title("Projects").
		Headers("PROJECT", "SOURCE", "LABELS", "VULNS", "VIOLATIONS", "CREATED").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No projects match. Sync repositories with: safedep project sync")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), pluralProjects(len(rows)))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *projectListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("project_id\tname\tsource\tlabels\tvulnerabilities\tpolicy_violations\tcreated_at")
	for _, p := range r.Projects {
		b.WriteString("\n" + tui.PlainRow(p.ID, p.Name, p.Source, strings.Join(p.Labels, ","),
			countCell(p.Vulnerabilities), countCell(p.PolicyViolations), p.CreatedAt))
	}
	return b.String()
}

func pluralProjects(n int) string {
	if n == 1 {
		return "project"
	}
	return "projects"
}
