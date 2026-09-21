// Package paging holds the cursor-pagination helpers shared by every
// domain that walks a Control Tower list RPC. It lives outside internal/cmd
// because sibling command packages must not import each other.
package paging

import (
	"context"
	"fmt"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
)

// Paginate walks a cursor-paginated list RPC until the server stops handing
// out a next page token. fetch receives the current page token and returns
// the next one. Returning an empty token from fetch also stops the walk,
// which lets a caller finish early once it has everything it needs. The
// repeated-token guard keeps a server that cycles tokens from looping
// forever.
func Paginate(
	ctx context.Context,
	label string,
	fetch func(ctx context.Context, pageToken string) (string, error),
) error {
	pageToken := ""
	seen := map[string]struct{}{}
	for {
		next, err := fetch(ctx, pageToken)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if _, repeated := seen[next]; repeated {
			return fmt.Errorf("%s: invalid response: repeated page token", label)
		}
		seen[next] = struct{}{}
		pageToken = next
	}
}

// NewPaginationRequest builds a PaginationRequest, leaving zero values unset
// so the server applies its defaults.
func NewPaginationRequest(pageSize uint32, pageToken string) *messagescontroltowerv1.PaginationRequest {
	pagination := &messagescontroltowerv1.PaginationRequest{}
	if pageSize > 0 {
		pagination.SetPageSize(pageSize)
	}
	if pageToken != "" {
		pagination.SetPageToken(pageToken)
	}
	return pagination
}
