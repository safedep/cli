// Package bitbucketlink resolves a tenant's Bitbucket workspace links. It
// lives outside internal/cmd because both the project and the integration
// command trees need it, and sibling command packages must not import each
// other.
package bitbucketlink

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/paging"
)

// PageSize is the integration service's cap on a listing page.
const PageSize = 100

// Lister is the one RPC this package reads.
type Lister interface {
	ListBitbucketWorkspaceLinks(
		context.Context,
		*controltowerv1.ListBitbucketWorkspaceLinksRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListBitbucketWorkspaceLinksResponse, error)
}

// Link is one Bitbucket workspace link of the tenant.
type Link struct {
	ID            string
	WorkspaceUUID string
	WorkspaceSlug string
	WorkspaceName string
}

// List walks every page of the tenant's workspace links.
func List(ctx context.Context, client Lister, label string) ([]Link, error) {
	var links []Link
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListBitbucketWorkspaceLinksRequest{}
		req.SetPagination(paging.NewPaginationRequest(PageSize, pageToken))
		res, err := client.ListBitbucketWorkspaceLinks(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		for i, item := range res.GetLinks() {
			if item.GetLinkId() == "" {
				return "", fmt.Errorf("%s: invalid response: link %d is missing its ID", label, i+1)
			}
			links = append(links, Link{
				ID:            item.GetLinkId(),
				WorkspaceUUID: item.GetWorkspaceUuid(),
				WorkspaceSlug: item.GetWorkspaceSlug(),
				WorkspaceName: item.GetWorkspaceName(),
			})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, err
	}
	return links, nil
}

// ResolveSingle returns the tenant's only workspace link when the caller did
// not pick one with --link-id. With no link or more than one, it fails with
// guidance, mirroring the GitHub link resolution in `project sync`.
func ResolveSingle(ctx context.Context, client Lister, label string) (string, error) {
	links, err := List(ctx, client, label)
	if err != nil {
		return "", err
	}
	return PickSingle(links, label)
}

// PickSingle applies ResolveSingle's selection to links a caller already
// fetched, so one listing serves both the source inference and the link
// resolution in `project sync`.
func PickSingle(links []Link, label string) (string, error) {
	switch len(links) {
	case 0:
		return "", usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrNotFound).
			WithHumanError("No linked Bitbucket workspace").
			WithHelp("Create a link code with `safedep integration bitbucket link create`, redeem it in the " +
				"workspace's SafeDep settings page in Bitbucket, then retry.").
			Wrap(fmt.Errorf("%s: the active tenant has no linked bitbucket workspace", label))
	case 1:
		return links[0].ID, nil
	default:
		return "", ambiguousLinkError(links)
	}
}

func ambiguousLinkError(links []Link) error {
	choices := make([]string, 0, len(links))
	for _, link := range links {
		slug := link.WorkspaceSlug
		if slug == "" {
			slug = link.WorkspaceUUID
		}
		choices = append(choices, fmt.Sprintf("%s (%s)", link.ID, slug))
	}
	sort.Strings(choices)
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError("More than one linked Bitbucket workspace").
		WithHelp(fmt.Sprintf("Pick one with --link-id: %s.", strings.Join(choices, ", "))).
		Wrap(errors.New("the active tenant has more than one linked bitbucket workspace"))
}
