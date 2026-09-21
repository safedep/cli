package bitbucket

import (
	"context"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/bitbucketlink"
)

// Every list RPC in the integration service caps a page at 100 items.
const pageSize = bitbucketlink.PageSize

type linkCodeCreator interface {
	CreateBitbucketWorkspaceLinkCode(
		context.Context,
		*controltowerv1.CreateBitbucketWorkspaceLinkCodeRequest,
		...grpc.CallOption,
	) (*controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse, error)
}

type linkLister = bitbucketlink.Lister

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

type workspaceLink = bitbucketlink.Link

func listWorkspaceLinks(ctx context.Context, client linkLister, label string) ([]workspaceLink, error) {
	return bitbucketlink.List(ctx, client, label)
}

func resolveLinkID(ctx context.Context, client linkLister, label string) (string, error) {
	return bitbucketlink.ResolveSingle(ctx, client, label)
}
