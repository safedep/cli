package project

import (
	"context"
	"fmt"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/sourcelink"
)

const (
	githubLinkPageSize = 100

	// ListGitHubInstallationRepositories caps a page at 100 repositories.
	githubRepositoryPageSize = 100
)

type githubLinkLister interface {
	ListGitHubAppInstallationLinks(
		context.Context,
		*controltowerv1.ListGitHubAppInstallationLinksRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListGitHubAppInstallationLinksResponse, error)
}

type githubRepositoryLister interface {
	ListGitHubInstallationRepositories(
		context.Context,
		*controltowerv1.ListGitHubInstallationRepositoriesRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListGitHubInstallationRepositoriesResponse, error)
}

type githubProjectSyncer interface {
	SyncGitHubInstallationProjects(
		context.Context,
		*controltowerv1.SyncGitHubInstallationProjectsRequest,
		...grpc.CallOption,
	) (*controltowerv1.SyncGitHubInstallationProjectsResponse, error)
}

type githubRepository struct {
	id       int64
	fullName string
}

func newIntegrationClient(conn grpc.ClientConnInterface) controltowerv1grpc.IntegrationServiceClient {
	return controltowerv1grpc.NewIntegrationServiceClient(conn)
}

// githubRequest is one SyncGitHubInstallationProjects call: the link, the
// repository IDs in request order, and the names the CLI resolved.
type githubRequest struct {
	link  sourcelink.Link
	ids   []int64
	names map[int64]string
}

// prepareGitHubRequest resolves the plan's names through its link. It sends
// no write, so a run resolves every plan before it syncs any.
func prepareGitHubRequest(
	ctx context.Context,
	repositories githubRepositoryLister,
	plan syncPlan,
) (githubRequest, error) {
	resolved, err := resolveRepositoryNames(ctx, repositories, plan.link, plan.names)
	if err != nil {
		return githubRequest{}, err
	}

	// Flag-selected IDs go first, then resolved names in argument order, so the
	// request order matches what the caller typed.
	req := githubRequest{
		link:  plan.link,
		ids:   append([]int64{}, plan.ids...),
		names: make(map[int64]string, len(resolved)),
	}
	seen := make(map[int64]struct{}, len(req.ids)+len(resolved))
	for _, repositoryID := range req.ids {
		seen[repositoryID] = struct{}{}
	}
	for _, repository := range resolved {
		if _, duplicate := seen[repository.id]; duplicate {
			cause := fmt.Errorf(
				"duplicate repository ID %d after name resolution: %q",
				repository.id,
				repository.fullName,
			)
			return githubRequest{}, invalidRepositorySelectionError(cause, fmt.Sprintf(
				"Repository %q resolves to repository ID %d, which is already selected. Remove one of them and retry.",
				repository.fullName,
				repository.id,
			))
		}
		seen[repository.id] = struct{}{}
		req.names[repository.id] = repository.fullName
		req.ids = append(req.ids, repository.id)
	}
	return req, nil
}

func listGitHubLinks(ctx context.Context, client githubLinkLister) ([]sourcelink.Link, error) {
	const label = "project sync: resolve installation link"

	var links []sourcelink.Link
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListGitHubAppInstallationLinksRequest{}
		req.SetPagination(paging.NewPaginationRequest(githubLinkPageSize, pageToken))
		res, err := client.ListGitHubAppInstallationLinks(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		for i, item := range res.GetIntegrations() {
			if item.GetLinkId() == "" {
				return "", fmt.Errorf("%s: invalid response: link %d is missing its ID", label, i+1)
			}
			links = append(links, sourcelink.Link{
				Source:  sourcelink.GitHub,
				ID:      item.GetLinkId(),
				Account: item.GetAccountLogin(),
			})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, err
	}

	return links, nil
}

// resolveRepositoryNames maps owner/repository names to immutable GitHub
// repository IDs. The listing RPC is an unfiltered proxy over the installation,
// so the walk stops as soon as every requested name has a match.
func resolveRepositoryNames(
	ctx context.Context,
	client githubRepositoryLister,
	link sourcelink.Link,
	names []string,
) ([]githubRepository, error) {
	if len(names) == 0 {
		return nil, nil
	}

	const label = "project sync: resolve repositories"
	requested := make(map[string]struct{}, len(names))
	for _, name := range names {
		requested[repositoryKey(name)] = struct{}{}
	}

	matches := make(map[string]githubRepository, len(names))
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListGitHubInstallationRepositoriesRequest{}
		req.SetLinkId(link.ID)
		req.SetPagination(paging.NewPaginationRequest(githubRepositoryPageSize, pageToken))
		res, err := client.ListGitHubInstallationRepositories(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		if err := collectRepositoryMatches(res, label, requested, matches); err != nil {
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

	resolved := make([]githubRepository, 0, len(names))
	for _, name := range names {
		match, ok := matches[repositoryKey(name)]
		if !ok {
			return nil, repositoryNotAccessibleError(name, link)
		}
		resolved = append(resolved, match)
	}
	return resolved, nil
}

func collectRepositoryMatches(
	res *controltowerv1.ListGitHubInstallationRepositoriesResponse,
	label string,
	requested map[string]struct{},
	matches map[string]githubRepository,
) error {
	for i, repository := range res.GetRepositories() {
		if err := checkGitHubRepository(repository, label, i); err != nil {
			return err
		}
		key := repositoryKey(repository.GetFullName())
		if _, ok := requested[key]; !ok {
			continue
		}
		matches[key] = githubRepository{
			id:       repository.GetRepositoryId(),
			fullName: repository.GetFullName(),
		}
	}
	return nil
}

func checkGitHubRepository(
	repository *controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository,
	label string,
	i int,
) error {
	if repository.GetRepositoryId() <= 0 || repository.GetFullName() == "" {
		return fmt.Errorf("%s: invalid response: repository %d is missing its identity", label, i+1)
	}
	return nil
}

// listAllGitHubRepositories walks every page of the installation's
// repositories. A repository on two pages counts once, because the listing
// proxies GitHub and a page boundary can move while the walk runs.
func listAllGitHubRepositories(
	ctx context.Context,
	client githubRepositoryLister,
	linkID string,
) ([]githubRepository, error) {
	const label = "project sync: list repositories"

	var repositories []githubRepository
	seen := map[int64]struct{}{}
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListGitHubInstallationRepositoriesRequest{}
		req.SetLinkId(linkID)
		req.SetPagination(paging.NewPaginationRequest(githubRepositoryPageSize, pageToken))
		res, err := client.ListGitHubInstallationRepositories(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		for i, repository := range res.GetRepositories() {
			if err := checkGitHubRepository(repository, label, i); err != nil {
				return "", err
			}
			if _, duplicate := seen[repository.GetRepositoryId()]; duplicate {
				continue
			}
			seen[repository.GetRepositoryId()] = struct{}{}
			repositories = append(repositories, githubRepository{
				id:       repository.GetRepositoryId(),
				fullName: repository.GetFullName(),
			})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, err
	}
	return repositories, nil
}

func repositoryNotAccessibleError(name string, link sourcelink.Link) error {
	cause := fmt.Errorf(
		"project sync: repository %q is not accessible through installation link %q",
		name,
		link.ID,
	)
	return newProjectError(
		usefulerror.ErrNotFound,
		"GitHub repository not accessible",
		fmt.Sprintf("Grant %q to the GitHub App installation of %s, or retry with --repository-id.",
			name, sourcelink.Describe(link.Account, link.ID)),
		cause,
	)
}

func syncGitHubProjects(
	ctx context.Context,
	client githubProjectSyncer,
	req githubRequest,
) ([]syncedProject, error) {
	res, err := client.SyncGitHubInstallationProjects(ctx, newSyncRequest(req.link.ID, req.ids))
	if err != nil {
		return nil, fmt.Errorf("project sync: %w", err)
	}
	return translateSyncResponse(res, req)
}

func newSyncRequest(linkID string, repositoryIDs []int64) *controltowerv1.SyncGitHubInstallationProjectsRequest {
	selections := make([]*controltowerv1.SyncGitHubInstallationProjectsRequest_RepositorySelection, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		selection := &controltowerv1.SyncGitHubInstallationProjectsRequest_RepositorySelection{}
		selection.SetRepositoryId(repositoryID)
		selections = append(selections, selection)
	}
	req := &controltowerv1.SyncGitHubInstallationProjectsRequest{}
	req.SetLinkId(linkID)
	req.SetRepositories(selections)
	return req
}

func translateSyncResponse(
	res *controltowerv1.SyncGitHubInstallationProjectsResponse,
	req githubRequest,
) ([]syncedProject, error) {
	repositoryIDs := req.ids
	mappings := res.GetProjects()
	if len(mappings) != len(repositoryIDs) {
		return nil, fmt.Errorf(
			"project sync: invalid response: response count %d does not match request count %d",
			len(mappings),
			len(repositoryIDs),
		)
	}

	requested := make(map[int64]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		requested[repositoryID] = struct{}{}
	}

	seen := make(map[int64]struct{}, len(mappings))
	projects := make([]syncedProject, 0, len(mappings))
	for _, mapping := range mappings {
		repositoryID := mapping.GetRepositoryId()
		if _, ok := requested[repositoryID]; !ok {
			return nil, fmt.Errorf(
				"project sync: invalid response: unexpected repository ID %d",
				repositoryID,
			)
		}
		if _, duplicate := seen[repositoryID]; duplicate {
			return nil, fmt.Errorf(
				"project sync: invalid response: duplicate repository ID %d",
				repositoryID,
			)
		}
		seen[repositoryID] = struct{}{}

		if mapping.GetProjectId() == "" {
			return nil, fmt.Errorf(
				"project sync: invalid response: repository ID %d is missing its project ID",
				repositoryID,
			)
		}
		projects = append(projects, syncedProject{
			RepositoryID:   repositoryID,
			RepositoryName: req.names[repositoryID],
			ProjectID:      mapping.GetProjectId(),
			Account:        req.link.Account,
			LinkID:         req.link.ID,
		})
	}
	return projects, nil
}
