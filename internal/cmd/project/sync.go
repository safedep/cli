package project

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

const (
	// SyncGitHubInstallationProjects accepts between one and one hundred
	// repository selections per request.
	maxSyncRepositories = 100

	// unknownRepositoryName marks a project materialized from a repository ID
	// the caller supplied directly, so the CLI never resolved its name.
	unknownRepositoryName = "-"
)

type syncInput struct {
	LinkID          string
	RepositoryNames []string
	RepositoryIDs   []int64
}

// syncedProject carries its own JSON tags because the wire shape and the
// display shape are identical here, unlike the scan results whose timestamps and
// URLs need reshaping.
type syncedProject struct {
	RepositoryID   int64  `json:"repository_id"`
	RepositoryName string `json:"repository_name,omitempty"`
	ProjectID      string `json:"project_id"`
}

type syncResult struct {
	linkID   string
	projects []syncedProject
}

type syncResultJSON struct {
	LinkID   string          `json:"link_id"`
	Projects []syncedProject `json:"projects"`
}

func syncCmd(a *app.App) *cobra.Command {
	var in syncInput
	cmd := &cobra.Command{
		Use:   "sync [OWNER/REPOSITORY...]",
		Short: "Sync GitHub repositories into SafeDep projects",
		Long: "Materialize one SafeDep project per GitHub repository reachable through a linked " +
			"GitHub App installation. Repository names are resolved to immutable GitHub " +
			"repository IDs before the request, and the sync is idempotent: repeating it " +
			"returns the same project for the same repository.",
		Args: func(_ *cobra.Command, args []string) error {
			in.RepositoryNames = args
			return validateSyncInput(in)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			in.RepositoryNames = args
			integration := newIntegrationClient(client.Connection())
			result, err := runSync(cmd.Context(), integration, integration, integration, in)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	f := cmd.Flags()
	f.StringVar(&in.LinkID, "link-id", "",
		"GitHub App installation link to sync through; resolved automatically when the tenant has exactly one link")
	f.Int64SliceVar(&in.RepositoryIDs, "repository-id", nil,
		"GitHub repository ID to sync instead of a name; repeat for multiple repositories")
	// pflag renders an empty slice default as "(default [])", which reads as a
	// value the flag accepts. An empty DefValue suppresses the default in help
	// without changing the flag's zero value.
	f.Lookup("repository-id").DefValue = ""
	return cmd
}

func validateSyncInput(in syncInput) error {
	total := len(in.RepositoryIDs) + len(in.RepositoryNames)
	if total < 1 || total > maxSyncRepositories {
		cause := fmt.Errorf("project sync requires between 1 and %d repositories", maxSyncRepositories)
		return invalidRepositorySelectionError(
			cause,
			fmt.Sprintf("Provide between 1 and %d repository names or --repository-id values.", maxSyncRepositories),
		)
	}
	if err := validateRepositoryNames(in.RepositoryNames); err != nil {
		return err
	}
	return validateRepositoryIDs(in.RepositoryIDs)
}

// validateRepositoryNames enforces the owner/repository shape and rejects
// duplicates case-insensitively, because GitHub treats owner and repository
// names as case-insensitive.
func validateRepositoryNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for i, name := range names {
		owner, repo, found := strings.Cut(name, "/")
		if !found || owner == "" || repo == "" || strings.Contains(repo, "/") {
			cause := fmt.Errorf("repository at position %d must use the OWNER/REPOSITORY form, got %q", i+1, name)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Provide repository %d as OWNER/REPOSITORY, for example safedep/cli.", i+1),
			)
		}

		key := repositoryKey(name)
		if _, duplicate := seen[key]; duplicate {
			cause := fmt.Errorf("duplicate repository %q", name)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Remove duplicate repository %q and retry.", name),
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateRepositoryIDs(ids []int64) error {
	seen := make(map[int64]struct{}, len(ids))
	for i, id := range ids {
		if id <= 0 {
			cause := fmt.Errorf("repository ID at position %d must be greater than zero, got %d", i+1, id)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Provide a positive GitHub repository ID at position %d.", i+1),
			)
		}
		if _, duplicate := seen[id]; duplicate {
			cause := fmt.Errorf("duplicate repository ID %d", id)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Remove duplicate repository ID %d and retry.", id),
			)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func repositoryKey(name string) string {
	return strings.ToLower(name)
}

func (r *syncResult) RenderJSON() ([]byte, error) {
	projects := r.projects
	if projects == nil {
		projects = []syncedProject{}
	}
	return json.MarshalIndent(syncResultJSON{LinkID: r.linkID, Projects: projects}, "", "  ")
}

func (r *syncResult) RenderPlain() string {
	var output strings.Builder
	output.WriteString("repository_name\trepository_id\tproject_id")
	for _, project := range r.projects {
		output.WriteByte('\n')
		output.WriteString(strings.Join(syncedProjectCells(project), "\t"))
	}
	return output.String()
}

func (r *syncResult) RenderTable() string {
	rows := make([][]string, 0, len(r.projects))
	for _, project := range r.projects {
		rows = append(rows, syncedProjectCells(project))
	}
	return table.New().
		Title("Synced projects").
		Headers("REPOSITORY", "REPOSITORY ID", "PROJECT ID").
		Rows(rows...).
		Footer(fmt.Sprintf("%d %s synced through link %s",
			len(rows), pluralRepositories(len(rows)), r.linkID)).
		Render()
}

func syncedProjectCells(project syncedProject) []string {
	name := project.RepositoryName
	if name == "" {
		name = unknownRepositoryName
	}
	return []string{
		name,
		strconv.FormatInt(project.RepositoryID, 10),
		project.ProjectID,
	}
}

func pluralRepositories(n int) string {
	if n == 1 {
		return "repository"
	}
	return "repositories"
}
