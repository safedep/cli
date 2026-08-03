package project

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc"
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

type githubLink struct {
	id           string
	accountLogin string
}

type githubRepository struct {
	id       int64
	fullName string
}

func newIntegrationClient(conn grpc.ClientConnInterface) controltowerv1grpc.IntegrationServiceClient {
	return controltowerv1grpc.NewIntegrationServiceClient(conn)
}

func runSync(
	ctx context.Context,
	links githubLinkLister,
	repositories githubRepositoryLister,
	syncer githubProjectSyncer,
	in syncInput,
) (*syncResult, error) {
	if err := validateSyncInput(in); err != nil {
		return nil, err
	}

	linkID := in.LinkID
	if linkID == "" {
		resolved, err := resolveGitHubLink(ctx, links)
		if err != nil {
			return nil, err
		}
		linkID = resolved
	}

	resolved, err := resolveRepositoryNames(ctx, repositories, linkID, in.RepositoryNames)
	if err != nil {
		return nil, err
	}

	// Flag-selected IDs go first, then resolved names in argument order, so the
	// request order matches what the caller typed.
	repositoryIDs := append([]int64{}, in.RepositoryIDs...)
	namesByID := make(map[int64]string, len(resolved))
	seen := make(map[int64]struct{}, len(repositoryIDs)+len(resolved))
	for _, repositoryID := range repositoryIDs {
		seen[repositoryID] = struct{}{}
	}
	for _, repository := range resolved {
		if _, duplicate := seen[repository.id]; duplicate {
			cause := fmt.Errorf(
				"duplicate repository ID %d after name resolution: %q",
				repository.id,
				repository.fullName,
			)
			return nil, invalidRepositorySelectionError(cause, fmt.Sprintf(
				"Repository %q resolves to repository ID %d, which is already selected. Remove one of them and retry.",
				repository.fullName,
				repository.id,
			))
		}
		seen[repository.id] = struct{}{}
		namesByID[repository.id] = repository.fullName
		repositoryIDs = append(repositoryIDs, repository.id)
	}

	return syncGitHubProjects(ctx, syncer, linkID, repositoryIDs, namesByID)
}

func resolveGitHubLink(ctx context.Context, client githubLinkLister) (string, error) {
	const label = "project sync: resolve installation link"

	var links []githubLink
	err := paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListGitHubAppInstallationLinksRequest{}
		req.SetPagination(newPaginationRequest(githubLinkPageSize, pageToken))
		res, err := client.ListGitHubAppInstallationLinks(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		for i, item := range res.GetIntegrations() {
			if item.GetLinkId() == "" {
				return "", fmt.Errorf("%s: invalid response: link %d is missing its ID", label, i+1)
			}
			links = append(links, githubLink{id: item.GetLinkId(), accountLogin: item.GetAccountLogin()})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return "", err
	}

	switch len(links) {
	case 0:
		return "", newProjectError(
			usefulerror.ErrNotFound,
			"No GitHub App installation link",
			"Install the SafeDep GitHub App and link it to this tenant, then retry.",
			errors.New("project sync: the active tenant has no GitHub App installation link"),
		)
	case 1:
		return links[0].id, nil
	default:
		return "", ambiguousGitHubLinkError(links)
	}
}

// resolveRepositoryNames maps owner/repository names to immutable GitHub
// repository IDs. The listing RPC is an unfiltered proxy over the installation,
// so the walk stops as soon as every requested name has a match.
func resolveRepositoryNames(
	ctx context.Context,
	client githubRepositoryLister,
	linkID string,
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
	err := paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListGitHubInstallationRepositoriesRequest{}
		req.SetLinkId(linkID)
		req.SetPagination(newPaginationRequest(githubRepositoryPageSize, pageToken))
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
			return nil, repositoryNotAccessibleError(name, linkID)
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
		if repository.GetRepositoryId() <= 0 || repository.GetFullName() == "" {
			return fmt.Errorf("%s: invalid response: repository %d is missing its identity", label, i+1)
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

func repositoryNotAccessibleError(name, linkID string) error {
	cause := fmt.Errorf(
		"project sync: repository %q is not accessible through installation link %q",
		name,
		linkID,
	)
	return newProjectError(
		usefulerror.ErrNotFound,
		"GitHub repository not accessible",
		fmt.Sprintf("Grant %q to the linked GitHub App installation, or retry with --repository-id.", name),
		cause,
	)
}

func syncGitHubProjects(
	ctx context.Context,
	client githubProjectSyncer,
	linkID string,
	repositoryIDs []int64,
	namesByID map[int64]string,
) (*syncResult, error) {
	res, err := client.SyncGitHubInstallationProjects(ctx, newSyncRequest(linkID, repositoryIDs))
	if err != nil {
		return nil, fmt.Errorf("project sync: %w", err)
	}
	projects, err := translateSyncResponse(res, repositoryIDs, namesByID)
	if err != nil {
		return nil, err
	}
	return &syncResult{linkID: linkID, projects: projects}, nil
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
	repositoryIDs []int64,
	namesByID map[int64]string,
) ([]syncedProject, error) {
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
			RepositoryName: namesByID[repositoryID],
			ProjectID:      mapping.GetProjectId(),
		})
	}
	return projects, nil
}

func ambiguousGitHubLinkError(links []githubLink) error {
	sort.Slice(links, func(i, j int) bool { return links[i].id < links[j].id })

	labels := make([]string, 0, len(links))
	for _, link := range links {
		if link.accountLogin == "" {
			labels = append(labels, link.id)
			continue
		}
		labels = append(labels, fmt.Sprintf("%s (%s)", link.id, link.accountLogin))
	}
	joined := strings.Join(labels, ", ")
	cause := fmt.Errorf(
		"project sync: the active tenant has %d GitHub App installation links: %s",
		len(links),
		joined,
	)
	return newProjectError(
		usefulerror.ErrBadRequest,
		"GitHub App installation link is ambiguous",
		"Retry with --link-id set to one of: "+joined+".",
		cause,
	)
}
