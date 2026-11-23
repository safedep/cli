package project

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/safedep/dry/tui/table"

	"github.com/safedep/cli/internal/sourcelink"
	"github.com/safedep/cli/internal/tui"
)

// syncedProject carries its own JSON tags because the wire shape and the
// display shape are identical here, unlike the scan results whose timestamps and
// URLs need reshaping.
type syncedProject struct {
	RepositoryID   int64  `json:"repository_id"`
	RepositoryName string `json:"repository_name,omitempty"`
	ProjectID      string `json:"project_id"`
	Account        string `json:"account,omitempty"`
	LinkID         string `json:"link_id"`
}

type syncedBitbucketProject struct {
	RepositoryUUID string `json:"repository_uuid"`
	RepositoryName string `json:"repository_name,omitempty"`
	ProjectID      string `json:"project_id"`
	Account        string `json:"account,omitempty"`
	LinkID         string `json:"link_id"`
}

// syncResult lists the links a run synced through in request order. all
// marks a run of --all, whose footer reports the total.
type syncResult struct {
	links    []sourcelink.Link
	projects []syncedProject
	all      bool
}

type bitbucketSyncResult struct {
	links      []sourcelink.Link
	projects   []syncedBitbucketProject
	all        bool
	notEnabled int
}

// syncResultJSON reports the link at the top level only when one link served
// the run. With several links, each project names its own.
type syncResultJSON[P any] struct {
	LinkID   string `json:"link_id,omitempty"`
	Account  string `json:"account,omitempty"`
	Projects []P    `json:"projects"`
}

func newSyncResultJSON[P any](links []sourcelink.Link, projects []P) syncResultJSON[P] {
	if projects == nil {
		projects = []P{}
	}
	out := syncResultJSON[P]{Projects: projects}
	if len(links) == 1 {
		out.LinkID = links[0].ID
		out.Account = links[0].Account
	}
	return out
}

func (r *syncResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(newSyncResultJSON(r.links, r.projects), "", "  ")
}

func (r *syncResult) RenderPlain() string {
	rows := make([][]string, 0, len(r.projects))
	for _, project := range r.projects {
		rows = append(rows, []string{
			orEmptyCell(project.RepositoryName),
			strconv.FormatInt(project.RepositoryID, 10),
			project.ProjectID,
			orEmptyCell(project.Account),
		})
	}
	return plainRows("repository_name\trepository_id\tproject_id\taccount", rows)
}

func (r *syncResult) RenderTable() string {
	rows := make([][]string, 0, len(r.projects))
	for _, project := range r.projects {
		rows = append(rows, []string{
			orEmptyCell(project.RepositoryName),
			strconv.FormatInt(project.RepositoryID, 10),
			project.ProjectID,
			sourcelink.Label(project.Account, project.LinkID),
		})
	}
	return table.New().
		Title("Synced projects").
		Headers("REPOSITORY", "REPOSITORY ID", "PROJECT ID", "ACCOUNT").
		Rows(rows...).
		Footer(syncFooter(len(rows), r.links, r.all, 0)).
		Render()
}

func (r *bitbucketSyncResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(newSyncResultJSON(r.links, r.projects), "", "  ")
}

func (r *bitbucketSyncResult) RenderPlain() string {
	rows := make([][]string, 0, len(r.projects))
	for _, project := range r.projects {
		rows = append(rows, []string{
			orEmptyCell(project.RepositoryName),
			project.RepositoryUUID,
			project.ProjectID,
			orEmptyCell(project.Account),
		})
	}
	return plainRows("repository_name\trepository_uuid\tproject_id\taccount", rows)
}

func (r *bitbucketSyncResult) RenderTable() string {
	rows := make([][]string, 0, len(r.projects))
	for _, project := range r.projects {
		rows = append(rows, []string{
			orEmptyCell(project.RepositoryName),
			project.RepositoryUUID,
			project.ProjectID,
			sourcelink.Label(project.Account, project.LinkID),
		})
	}
	return table.New().
		Title("Synced projects").
		Headers("REPOSITORY", "REPOSITORY UUID", "PROJECT ID", "ACCOUNT").
		Rows(rows...).
		Footer(syncFooter(len(rows), r.links, r.all, r.notEnabled)).
		Render()
}

// plainRows keeps the account last, so a pipeline that cuts the first three
// columns reads the same fields in every release.
func plainRows(header string, rows [][]string) string {
	var output strings.Builder
	output.WriteString(header)
	for _, row := range rows {
		output.WriteByte('\n')
		output.WriteString(strings.Join(row, "\t"))
	}
	return output.String()
}

// orEmptyCell fills the name of a repository selected by ID or UUID, which
// the CLI never resolved, and the account of a link without an account name.
func orEmptyCell(value string) string {
	if value == "" {
		return tui.EmptyCell
	}
	return value
}

func syncFooter(synced int, links []sourcelink.Link, all bool, notEnabled int) string {
	through := fmt.Sprintf("%d accounts", len(links))
	if len(links) == 1 {
		through = sourcelink.Describe(links[0].Account, links[0].ID)
	}
	switch {
	case !all:
		return fmt.Sprintf("%d %s synced through %s", synced, pluralRepositories(synced), through)
	case notEnabled == 0:
		return fmt.Sprintf("Synced %d %s through %s", synced, pluralRepositories(synced), through)
	default:
		return fmt.Sprintf("Synced %d of %d repositories through %s, %d not enabled",
			synced, synced+notEnabled, through, notEnabled)
	}
}

func pluralRepositories(n int) string {
	if n == 1 {
		return "repository"
	}
	return "repositories"
}
