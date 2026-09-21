package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/safedep/dry/tui/table"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/bitbucketlink"
	"github.com/safedep/cli/internal/tui"
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
	Source          string
	LinkID          string
	RepositoryNames []string
	RepositoryIDs   []int64
	RepositoryUUIDs []string
}

const (
	sourceGitHub    = "github"
	sourceBitbucket = "bitbucket"
)

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
		Short: "Sync GitHub or Bitbucket repositories into SafeDep projects",
		Long: "Materialize one SafeDep project per repository reachable through a linked " +
			"source integration: a GitHub App installation or a Bitbucket workspace. " +
			"Repository names are resolved to the source's immutable identity before the " +
			"request, and the sync is idempotent: repeating it returns the same project " +
			"for the same repository. When --source is omitted, the CLI uses the one " +
			"source the tenant has links for.",
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
			deps := syncDeps{
				githubLinks:           integration,
				githubRepositories:    integration,
				githubSyncer:          integration,
				bitbucketLinks:        integration,
				bitbucketRepositories: integration,
				bitbucketSyncer:       integration,
			}
			result, err := runSync(cmd.Context(), deps, in)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	f := cmd.Flags()
	f.StringVar(&in.Source, "source", "",
		"repository source: github or bitbucket (inferred from the tenant's links, or from --repository-id and --repository-uuid, when omitted)")
	f.StringVar(&in.LinkID, "link-id", "",
		"source link to sync through; resolved automatically when the tenant has exactly one link")
	f.Int64SliceVar(&in.RepositoryIDs, "repository-id", nil,
		"GitHub repository ID to sync instead of a name; repeat for multiple repositories")
	f.StringArrayVar(&in.RepositoryUUIDs, "repository-uuid", nil,
		"Bitbucket repository UUID to sync instead of a name; repeat for multiple repositories")
	// pflag renders an empty slice default as "(default [])", which reads as a
	// value the flag accepts. An empty DefValue suppresses the default in help
	// without changing the flag's zero value.
	f.Lookup("repository-id").DefValue = ""
	f.Lookup("repository-uuid").DefValue = ""
	return cmd
}

func validateSyncInput(in syncInput) error {
	if in.Source != "" && in.Source != sourceGitHub && in.Source != sourceBitbucket {
		cause := fmt.Errorf("unknown --source value %q: allowed values are github, bitbucket", in.Source)
		return invalidRepositorySelectionError(cause, "Retry --source with github or bitbucket.")
	}
	if len(in.RepositoryIDs) > 0 && len(in.RepositoryUUIDs) > 0 {
		cause := fmt.Errorf("--repository-id selects GitHub and --repository-uuid selects Bitbucket")
		return invalidRepositorySelectionError(cause, "One sync serves one source. Drop one of the flags and retry.")
	}
	if in.Source == sourceGitHub && len(in.RepositoryUUIDs) > 0 {
		cause := fmt.Errorf("--repository-uuid selects Bitbucket repositories")
		return invalidRepositorySelectionError(cause, "Drop --repository-uuid, or use --source bitbucket.")
	}
	if in.Source == sourceBitbucket && len(in.RepositoryIDs) > 0 {
		cause := fmt.Errorf("--repository-id selects GitHub repositories")
		return invalidRepositorySelectionError(cause, "Drop --repository-id, or use --source github.")
	}

	total := len(in.RepositoryIDs) + len(in.RepositoryNames) + len(in.RepositoryUUIDs)
	if total < 1 || total > maxSyncRepositories {
		cause := fmt.Errorf("project sync requires between 1 and %d repositories", maxSyncRepositories)
		return invalidRepositorySelectionError(
			cause,
			fmt.Sprintf("Provide between 1 and %d repository names, --repository-id, or --repository-uuid values.", maxSyncRepositories),
		)
	}
	if err := validateRepositoryNames(in.RepositoryNames); err != nil {
		return err
	}
	if err := validateUniqueValues(in.RepositoryUUIDs, "repository UUID"); err != nil {
		return err
	}
	return validateRepositoryIDs(in.RepositoryIDs)
}

type syncDeps struct {
	githubLinks           githubLinkLister
	githubRepositories    githubRepositoryLister
	githubSyncer          githubProjectSyncer
	bitbucketLinks        bitbucketlink.Lister
	bitbucketRepositories bitbucketRepositoryLister
	bitbucketSyncer       bitbucketProjectSyncer
}

func runSync(ctx context.Context, deps syncDeps, in syncInput) (tui.Renderable, error) {
	if err := validateSyncInput(in); err != nil {
		return nil, err
	}

	source, linkID, err := resolveSyncSource(ctx, deps, in)
	if err != nil {
		return nil, err
	}
	in.LinkID = linkID

	if source == sourceBitbucket {
		return runBitbucketSync(ctx, deps.bitbucketLinks, deps.bitbucketRepositories,
			deps.bitbucketSyncer, in)
	}
	return runGitHubSync(ctx, deps.githubLinks, deps.githubRepositories,
		deps.githubSyncer, in)
}

// resolveSyncSource picks the repository source and the link to sync
// through. An explicit --source wins, then the source a selector flag
// implies; those paths list nothing and keep the caller's --link-id, so the
// per-source path resolves the link with its own single walk. With names
// only, the tenant's links decide: --link-id picks the source that owns it,
// a tenant with one linked source uses that source, and a tenant with both
// must pick one. The listings also resolve the link, so the per-source path
// never repeats the walk.
func resolveSyncSource(ctx context.Context, deps syncDeps, in syncInput) (string, string, error) {
	if in.Source != "" {
		return in.Source, in.LinkID, nil
	}
	if len(in.RepositoryIDs) > 0 {
		return sourceGitHub, in.LinkID, nil
	}
	if len(in.RepositoryUUIDs) > 0 {
		return sourceBitbucket, in.LinkID, nil
	}

	githubLinks, err := listGitHubLinks(ctx, deps.githubLinks)
	if err != nil {
		return "", "", err
	}
	bitbucketLinks, err := listBitbucketLinksForInference(ctx, deps.bitbucketLinks)
	if err != nil {
		return "", "", err
	}

	if in.LinkID != "" {
		return sourceOwningLink(in.LinkID, githubLinks, bitbucketLinks)
	}

	switch {
	case len(githubLinks) > 0 && len(bitbucketLinks) > 0:
		cause := errors.New("project sync: the active tenant has GitHub and Bitbucket links")
		return "", "", invalidRepositorySelectionError(cause,
			"Pass --source github or --source bitbucket.")
	case len(bitbucketLinks) > 0:
		linkID, err := bitbucketlink.PickSingle(bitbucketLinks, syncLinkLabel)
		if err != nil {
			return "", "", err
		}
		return sourceBitbucket, linkID, nil
	case len(githubLinks) > 0:
		linkID, err := pickGitHubLink(githubLinks)
		if err != nil {
			return "", "", err
		}
		return sourceGitHub, linkID, nil
	default:
		return "", "", newProjectError(
			usefulerror.ErrNotFound,
			"No source integration link",
			"Install and link the SafeDep GitHub App, or link a Bitbucket workspace, then retry.",
			errors.New("project sync: the active tenant has no GitHub or Bitbucket link"),
		)
	}
}

const syncLinkLabel = "project sync: resolve workspace link"

// listBitbucketLinksForInference treats a control plane without the
// Bitbucket RPCs, or a caller without the permission for them, as a tenant
// with no Bitbucket links. Without this, a GitHub-only tenant against such a
// control plane loses every names-only sync to the failing listing. Any
// other failure leaves the source undecidable and stops the sync.
func listBitbucketLinksForInference(ctx context.Context,
	client bitbucketlink.Lister,
) ([]bitbucketlink.Link, error) {
	links, err := bitbucketlink.List(ctx, client, syncLinkLabel)
	switch status.Code(err) {
	case codes.Unimplemented, codes.PermissionDenied:
		return nil, nil
	}
	return links, err
}

// sourceOwningLink maps --link-id to the source that owns it, so a tenant
// with links to both sources can sync by name without --source.
func sourceOwningLink(linkID string, githubLinks []githubLink,
	bitbucketLinks []bitbucketlink.Link,
) (string, string, error) {
	for _, link := range githubLinks {
		if link.id == linkID {
			return sourceGitHub, linkID, nil
		}
	}
	for _, link := range bitbucketLinks {
		if link.ID == linkID {
			return sourceBitbucket, linkID, nil
		}
	}
	return "", "", newProjectError(
		usefulerror.ErrNotFound,
		"Source link not found",
		"List link IDs with `safedep integration bitbucket link list`, or drop --link-id to use the tenant's only link.",
		fmt.Errorf("project sync: link %q is not a GitHub or Bitbucket link of the active tenant", linkID),
	)
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
