package project

import (
	"context"
	"fmt"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/bitbucketlink"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/sourcelink"
)

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

// bitbucketRequest is one SyncBitbucketProjects call: the link, the
// repository UUIDs in request order, and the names the CLI resolved.
type bitbucketRequest struct {
	link  sourcelink.Link
	uuids []string
	names map[string]string
}

// prepareBitbucketRequest resolves the plan's names through its link. It
// sends no write, so a run resolves every plan before it syncs any.
func prepareBitbucketRequest(
	ctx context.Context,
	repositories bitbucketRepositoryLister,
	plan syncPlan,
) (bitbucketRequest, error) {
	resolved, err := resolveBitbucketRepositoryNames(ctx, repositories, plan.link, plan.names)
	if err != nil {
		return bitbucketRequest{}, err
	}

	// Flag-selected UUIDs go first, then resolved names in argument order,
	// so the request order matches what the caller typed.
	req := bitbucketRequest{
		link:  plan.link,
		uuids: append([]string{}, plan.uuids...),
		names: make(map[string]string, len(resolved)),
	}
	seen := make(map[string]struct{}, len(req.uuids)+len(resolved))
	for _, repositoryUUID := range req.uuids {
		seen[repositoryUUID] = struct{}{}
	}
	for _, repository := range resolved {
		if _, duplicate := seen[repository.uuid]; duplicate {
			cause := fmt.Errorf(
				"duplicate repository UUID %s after name resolution: %q",
				repository.uuid,
				repository.fullName,
			)
			return bitbucketRequest{}, invalidRepositorySelectionError(cause, fmt.Sprintf(
				"Repository %q resolves to repository UUID %s, which is already selected. Remove one of them and retry.",
				repository.fullName,
				repository.uuid,
			))
		}
		seen[repository.uuid] = struct{}{}
		req.names[repository.uuid] = repository.fullName
		req.uuids = append(req.uuids, repository.uuid)
	}
	return req, nil
}

// resolveBitbucketRepositoryNames maps workspace/repository names to
// immutable Bitbucket repository UUIDs. The listing RPC is an unfiltered
// walk over the workspace, so it stops as soon as every requested name has
// a match.
func resolveBitbucketRepositoryNames(
	ctx context.Context,
	client bitbucketRepositoryLister,
	link sourcelink.Link,
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
		req.SetLinkId(link.ID)
		req.SetPagination(paging.NewPaginationRequest(bitbucketlink.PageSize, pageToken))
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
			return nil, bitbucketRepositoryNotAccessibleError(name, link)
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
		if err := checkBitbucketRepository(repository, label, i); err != nil {
			return err
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

func checkBitbucketRepository(
	repository *controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository,
	label string,
	i int,
) error {
	if repository.GetRepositoryUuid() == "" || repository.GetFullName() == "" {
		return fmt.Errorf("%s: invalid response: repository %d is missing its identity", label, i+1)
	}
	return nil
}

// listEnabledBitbucketRepositories walks every page of the workspace's
// repositories and keeps those SafeDep scans. SyncBitbucketProjects refuses a
// repository whose scan state is not enabled, so a sync of the rest would
// fail the whole batch. It also returns how many repositories it left out.
// A repository on two pages counts once.
func listEnabledBitbucketRepositories(
	ctx context.Context,
	client bitbucketRepositoryLister,
	linkID string,
) ([]bitbucketRepository, int, error) {
	const label = "project sync: list repositories"

	var enabled []bitbucketRepository
	notEnabled := 0
	seen := map[string]struct{}{}
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListBitbucketRepositoriesRequest{}
		req.SetLinkId(linkID)
		req.SetPagination(paging.NewPaginationRequest(bitbucketlink.PageSize, pageToken))
		res, err := client.ListBitbucketRepositories(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		for i, repository := range res.GetRepositories() {
			if err := checkBitbucketRepository(repository, label, i); err != nil {
				return "", err
			}
			if _, duplicate := seen[repository.GetRepositoryUuid()]; duplicate {
				continue
			}
			seen[repository.GetRepositoryUuid()] = struct{}{}
			if repository.GetScanState() != controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_ENABLED {
				notEnabled++
				continue
			}
			enabled = append(enabled, bitbucketRepository{
				uuid:     repository.GetRepositoryUuid(),
				fullName: repository.GetFullName(),
			})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, 0, err
	}
	return enabled, notEnabled, nil
}

func bitbucketRepositoryNotAccessibleError(name string, link sourcelink.Link) error {
	cause := fmt.Errorf(
		"project sync: repository %q is not accessible through workspace link %q",
		name,
		link.ID,
	)
	return newProjectError(
		usefulerror.ErrNotFound,
		"Bitbucket repository not accessible",
		fmt.Sprintf("Confirm %q is in the workspace of %s, or retry with --repository-uuid.",
			name, sourcelink.Describe(link.Account, link.ID)),
		cause,
	)
}

func syncBitbucketProjects(
	ctx context.Context,
	client bitbucketProjectSyncer,
	req bitbucketRequest,
) ([]syncedBitbucketProject, error) {
	res, err := client.SyncBitbucketProjects(ctx, newBitbucketSyncRequest(req.link.ID, req.uuids))
	if err != nil {
		return nil, fmt.Errorf("project sync: %w", err)
	}
	return translateBitbucketSyncResponse(res, req)
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
	req bitbucketRequest,
) ([]syncedBitbucketProject, error) {
	repositoryUUIDs := req.uuids
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
			RepositoryName: req.names[repositoryUUID],
			ProjectID:      mapping.GetProjectId(),
			Account:        req.link.Account,
			LinkID:         req.link.ID,
		})
	}
	return projects, nil
}
