package bitbucket

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

	"github.com/safedep/cli/internal/paging"
)

// Every list RPC in the integration service caps a page at 100 items.
const pageSize = 100

type linkCodeCreator interface {
	CreateBitbucketWorkspaceLinkCode(
		context.Context,
		*controltowerv1.CreateBitbucketWorkspaceLinkCodeRequest,
		...grpc.CallOption,
	) (*controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse, error)
}

type linkLister interface {
	ListBitbucketWorkspaceLinks(
		context.Context,
		*controltowerv1.ListBitbucketWorkspaceLinksRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListBitbucketWorkspaceLinksResponse, error)
}

type repositoryLister interface {
	ListBitbucketRepositories(
		context.Context,
		*controltowerv1.ListBitbucketRepositoriesRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListBitbucketRepositoriesResponse, error)
}

type allowlistUpdater interface {
	UpdateBitbucketRepositoryAllowlist(
		context.Context,
		*controltowerv1.UpdateBitbucketRepositoryAllowlistRequest,
		...grpc.CallOption,
	) (*controltowerv1.UpdateBitbucketRepositoryAllowlistResponse, error)
}

func newIntegrationClient(conn grpc.ClientConnInterface) controltowerv1grpc.IntegrationServiceClient {
	return controltowerv1grpc.NewIntegrationServiceClient(conn)
}

type workspaceLink struct {
	linkID        string
	workspaceUUID string
	workspaceSlug string
	workspaceName string
}

func listWorkspaceLinks(ctx context.Context, client linkLister, label string) ([]workspaceLink, error) {
	var links []workspaceLink
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListBitbucketWorkspaceLinksRequest{}
		req.SetPagination(paging.NewPaginationRequest(pageSize, pageToken))
		res, err := client.ListBitbucketWorkspaceLinks(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		for i, item := range res.GetLinks() {
			if item.GetLinkId() == "" {
				return "", fmt.Errorf("%s: invalid response: link %d is missing its ID", label, i+1)
			}
			links = append(links, workspaceLink{
				linkID:        item.GetLinkId(),
				workspaceUUID: item.GetWorkspaceUuid(),
				workspaceSlug: item.GetWorkspaceSlug(),
				workspaceName: item.GetWorkspaceName(),
			})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, err
	}
	return links, nil
}

// resolveLinkID returns the tenant's only workspace link when the caller did
// not pick one with --link-id. With no link or more than one, it fails with
// guidance, mirroring the GitHub link resolution in `project sync`.
func resolveLinkID(ctx context.Context, client linkLister, label string) (string, error) {
	links, err := listWorkspaceLinks(ctx, client, label)
	if err != nil {
		return "", err
	}

	switch len(links) {
	case 0:
		return "", newBitbucketError(
			usefulerror.ErrNotFound,
			"No linked Bitbucket workspace",
			"Create a link code with `safedep integration bitbucket link create`, redeem it in the "+
				"workspace's SafeDep settings page in Bitbucket, then retry.",
			fmt.Errorf("%s: the active tenant has no linked bitbucket workspace", label),
		)
	case 1:
		return links[0].linkID, nil
	default:
		return "", ambiguousLinkError(links)
	}
}

func ambiguousLinkError(links []workspaceLink) error {
	choices := make([]string, 0, len(links))
	for _, link := range links {
		slug := link.workspaceSlug
		if slug == "" {
			slug = link.workspaceUUID
		}
		choices = append(choices, fmt.Sprintf("%s (%s)", link.linkID, slug))
	}
	sort.Strings(choices)
	return newBitbucketError(
		usefulerror.ErrBadRequest,
		"More than one linked Bitbucket workspace",
		fmt.Sprintf("Pick one with --link-id: %s.", strings.Join(choices, ", ")),
		errors.New("the active tenant has more than one linked bitbucket workspace"),
	)
}
