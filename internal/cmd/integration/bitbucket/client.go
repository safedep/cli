package bitbucket

import (
	"context"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc"
)

type linkCodeCreator interface {
	CreateBitbucketWorkspaceLinkCode(
		context.Context,
		*controltowerv1.CreateBitbucketWorkspaceLinkCodeRequest,
		...grpc.CallOption,
	) (*controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse, error)
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
