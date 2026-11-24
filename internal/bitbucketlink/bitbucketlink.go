// Package bitbucketlink resolves a tenant's Bitbucket workspace links. It
// lives outside internal/cmd because both the project and the integration
// command trees need it, and sibling command packages must not import each
// other.
package bitbucketlink

import (
	"context"
	"fmt"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/sourcelink"
)

// PageSize is the integration service's cap on a listing page.
const PageSize = 100

// NormalizeRepositoryUUID validates a repository UUID and returns the
// canonical lowercase, unbraced form the control plane requires. Bitbucket
// shows repository UUIDs braced and uppercase, so the CLI accepts those
// forms instead of letting the server reject them after auth and link
// resolution.
func NormalizeRepositoryUUID(value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid repository UUID %q: %w", value, err)
	}
	return parsed.String(), nil
}

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

// ToSourceLinks names each workspace link by its slug, the OWNER part of a
// Bitbucket repository name.
func ToSourceLinks(links []Link) []sourcelink.Link {
	out := make([]sourcelink.Link, 0, len(links))
	for _, link := range links {
		out = append(out, sourcelink.Link{Source: sourcelink.Bitbucket, ID: link.ID, Account: link.WorkspaceSlug})
	}
	return out
}

// Resolve returns the workspace link that account and linkID pick. A link
// ID alone skips the listing. The listing holds only active links, so the
// skip keeps a repeat delete of a revoked link a no-op, and a script that
// passes the ID keeps its RPC count. The returned link then carries only its
// ID.
func Resolve(ctx context.Context, client Lister, account, linkID, label string) (Link, error) {
	if linkID != "" && account == "" {
		return Link{ID: linkID}, nil
	}
	links, err := List(ctx, client, label)
	if err != nil {
		return Link{}, err
	}
	picked, err := sourcelink.Pick(ToSourceLinks(links), sourcelink.Selection{
		Source:  sourcelink.Bitbucket,
		Account: account,
		LinkID:  linkID,
	})
	if err != nil {
		return Link{}, err
	}
	byID := make(map[string]Link, len(links))
	for _, link := range links {
		byID[link.ID] = link
	}
	return byID[picked.ID], nil
}
