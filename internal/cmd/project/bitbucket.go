package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/bitbucketlink"
	"github.com/safedep/cli/internal/paging"
)

// ListBitbucketRepositories caps a page at 100 repositories.
const bitbucketRepositoryPageSize = 100

type bitbucketRepositoryLister interface {
	ListBitbucketRepositories(
		context.Context,
		*controltowerv1.ListBitbucketRepositoriesRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListBitbucketRepositoriesResponse, error)
}

type bitbucketProjectSyncer interface {
	SyncBitbucketProjects(
		context.Context,
		*controltowerv1.SyncBitbucketProjectsRequest,
		...grpc.CallOption,
	) (*controltowerv1.SyncBitbucketProjectsResponse, error)
}

type bitbucketRepository struct {
	uuid     string
	fullName string
}

type syncedBitbucketProject struct {
	RepositoryUUID string `json:"repository_uuid"`
	RepositoryName string `json:"repository_name,omitempty"`
	ProjectID      string `json:"project_id"`
}

type bitbucketSyncResult struct {
	linkID   string
	projects []syncedBitbucketProject
}

type bitbucketSyncResultJSON struct {
	LinkID   string                   `json:"link_id"`
	Projects []syncedBitbucketProject `json:"projects"`
}

func runBitbucketSync(
	ctx context.Context,
	links bitbucketlink.Lister,
	repositories bitbucketRepositoryLister,
	syncer bitbucketProjectSyncer,
	in syncInput,
) (*bitbucketSyncResult, error) {
	linkID := in.LinkID
	if linkID == "" {
		resolved, err := bitbucketlink.ResolveSingle(ctx, links, "project sync: resolve workspace link")
		if err != nil {
			return nil, err
		}
		linkID = resolved
	}

	resolved, err := resolveBitbucketRepositoryNames(ctx, repositories, linkID, in.RepositoryNames)
	if err != nil {
		return nil, err
	}

	// Flag-selected UUIDs go first, then resolved names in argument order,
	// so the request order matches what the caller typed.
	repositoryUUIDs := append([]string{}, in.RepositoryUUIDs...)
	namesByUUID := make(map[string]string, len(resolved))
	seen := make(map[string]struct{}, len(repositoryUUIDs)+len(resolved))
	for _, repositoryUUID := range repositoryUUIDs {
		seen[repositoryUUID] = struct{}{}
	}
	for _, repository := range resolved {
		if _, duplicate := seen[repository.uuid]; duplicate {
			cause := fmt.Errorf(
				"duplicate repository UUID %s after name resolution: %q",
				repository.uuid,
				repository.fullName,
			)
			return nil, invalidRepositorySelectionError(cause, fmt.Sprintf(
				"Repository %q resolves to repository UUID %s, which is already selected. Remove one of them and retry.",
				repository.fullName,
				repository.uuid,
			))
		}
		seen[repository.uuid] = struct{}{}
		namesByUUID[repository.uuid] = repository.fullName
		repositoryUUIDs = append(repositoryUUIDs, repository.uuid)
	}

	return syncBitbucketProjects(ctx, syncer, linkID, repositoryUUIDs, namesByUUID)
}

// resolveBitbucketRepositoryNames maps workspace/repository names to
// immutable Bitbucket repository UUIDs. The listing RPC is an unfiltered
// walk over the workspace, so it stops as soon as every requested name has
// a match.
func resolveBitbucketRepositoryNames(
	ctx context.Context,
	client bitbucketRepositoryLister,
	linkID string,
	names []string,
) ([]bitbucketRepository, error) {
	if len(names) == 0 {
		return nil, nil
	}

	const label = "project sync: resolve repositories"
	requested := make(map[string]struct{}, len(names))
	for _, name := range names {
		requested[repositoryKey(name)] = struct{}{}
	}

	matches := make(map[string]bitbucketRepository, len(names))
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListBitbucketRepositoriesRequest{}
		req.SetLinkId(linkID)
		req.SetPagination(paging.NewPaginationRequest(bitbucketRepositoryPageSize, pageToken))
		res, err := client.ListBitbucketRepositories(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		if err := collectBitbucketRepositoryMatches(res, label, requested, matches); err != nil {
			return "", err
		}
		if len(matches) == len(requested) {
			return "", nil
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, err
	}

	resolved := make([]bitbucketRepository, 0, len(names))
	for _, name := range names {
		match, ok := matches[repositoryKey(name)]
		if !ok {
			return nil, bitbucketRepositoryNotAccessibleError(name, linkID)
		}
		resolved = append(resolved, match)
	}
	return resolved, nil
}

func collectBitbucketRepositoryMatches(
	res *controltowerv1.ListBitbucketRepositoriesResponse,
	label string,
	requested map[string]struct{},
	matches map[string]bitbucketRepository,
) error {
	for i, repository := range res.GetRepositories() {
		if repository.GetRepositoryUuid() == "" || repository.GetFullName() == "" {
			return fmt.Errorf("%s: invalid response: repository %d is missing its identity", label, i+1)
		}
		key := repositoryKey(repository.GetFullName())
		if _, ok := requested[key]; !ok {
			continue
		}
		matches[key] = bitbucketRepository{
			uuid:     repository.GetRepositoryUuid(),
			fullName: repository.GetFullName(),
		}
	}
	return nil
}

func bitbucketRepositoryNotAccessibleError(name, linkID string) error {
	cause := fmt.Errorf(
		"project sync: repository %q is not accessible through workspace link %q",
		name,
		linkID,
	)
	return newProjectError(
		usefulerror.ErrNotFound,
		"Bitbucket repository not accessible",
		fmt.Sprintf("Confirm %q is in the linked workspace, or retry with --repository-uuid.", name),
		cause,
	)
}

func syncBitbucketProjects(
	ctx context.Context,
	client bitbucketProjectSyncer,
	linkID string,
	repositoryUUIDs []string,
	namesByUUID map[string]string,
) (*bitbucketSyncResult, error) {
	res, err := client.SyncBitbucketProjects(ctx, newBitbucketSyncRequest(linkID, repositoryUUIDs))
	if err != nil {
		return nil, fmt.Errorf("project sync: %w", err)
	}
	projects, err := translateBitbucketSyncResponse(res, repositoryUUIDs, namesByUUID)
	if err != nil {
		return nil, err
	}
	return &bitbucketSyncResult{linkID: linkID, projects: projects}, nil
}

func newBitbucketSyncRequest(linkID string, repositoryUUIDs []string) *controltowerv1.SyncBitbucketProjectsRequest {
	selections := make([]*controltowerv1.SyncBitbucketProjectsRequest_RepositorySelection, 0, len(repositoryUUIDs))
	for _, repositoryUUID := range repositoryUUIDs {
		selection := &controltowerv1.SyncBitbucketProjectsRequest_RepositorySelection{}
		selection.SetRepositoryUuid(repositoryUUID)
		selections = append(selections, selection)
	}
	req := &controltowerv1.SyncBitbucketProjectsRequest{}
	req.SetLinkId(linkID)
	req.SetRepositories(selections)
	return req
}

func translateBitbucketSyncResponse(
	res *controltowerv1.SyncBitbucketProjectsResponse,
	repositoryUUIDs []string,
	namesByUUID map[string]string,
) ([]syncedBitbucketProject, error) {
	mappings := res.GetProjects()
	if len(mappings) != len(repositoryUUIDs) {
		return nil, fmt.Errorf(
			"project sync: invalid response: response count %d does not match request count %d",
			len(mappings),
			len(repositoryUUIDs),
		)
	}

	requested := make(map[string]struct{}, len(repositoryUUIDs))
	for _, repositoryUUID := range repositoryUUIDs {
		requested[repositoryUUID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(mappings))
	projects := make([]syncedBitbucketProject, 0, len(mappings))
	for _, mapping := range mappings {
		repositoryUUID := mapping.GetRepositoryUuid()
		if _, ok := requested[repositoryUUID]; !ok {
			return nil, fmt.Errorf(
				"project sync: invalid response: unexpected repository UUID %s",
				repositoryUUID,
			)
		}
		if _, duplicate := seen[repositoryUUID]; duplicate {
			return nil, fmt.Errorf(
				"project sync: invalid response: duplicate repository UUID %s",
				repositoryUUID,
			)
		}
		if mapping.GetProjectId() == "" {
			return nil, fmt.Errorf(
				"project sync: invalid response: repository UUID %s is missing its project ID",
				repositoryUUID,
			)
		}
		seen[repositoryUUID] = struct{}{}
		projects = append(projects, syncedBitbucketProject{
			RepositoryUUID: repositoryUUID,
			RepositoryName: namesByUUID[repositoryUUID],
			ProjectID:      mapping.GetProjectId(),
		})
	}
	return projects, nil
}

func (r *bitbucketSyncResult) RenderJSON() ([]byte, error) {
	projects := r.projects
	if projects == nil {
		projects = []syncedBitbucketProject{}
	}
	return json.MarshalIndent(bitbucketSyncResultJSON{LinkID: r.linkID, Projects: projects}, "", "  ")
}

func (r *bitbucketSyncResult) RenderPlain() string {
	var output strings.Builder
	output.WriteString("repository_name\trepository_uuid\tproject_id")
	for _, project := range r.projects {
		output.WriteByte('\n')
		output.WriteString(strings.Join(syncedBitbucketProjectCells(project), "\t"))
	}
	return output.String()
}

func (r *bitbucketSyncResult) RenderTable() string {
	rows := make([][]string, 0, len(r.projects))
	for _, project := range r.projects {
		rows = append(rows, syncedBitbucketProjectCells(project))
	}
	return table.New().
		Title("Synced projects").
		Headers("REPOSITORY", "REPOSITORY UUID", "PROJECT ID").
		Rows(rows...).
		Footer(fmt.Sprintf("%d %s synced through link %s",
			len(rows), pluralRepositories(len(rows)), r.linkID)).
		Render()
}

func syncedBitbucketProjectCells(project syncedBitbucketProject) []string {
	name := project.RepositoryName
	if name == "" {
		name = unknownRepositoryName
	}
	return []string{
		name,
		project.RepositoryUUID,
		project.ProjectID,
	}
}
