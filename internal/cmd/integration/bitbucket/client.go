package bitbucket

import (
	"context"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

type linkCodeCreator interface {
	CreateBitbucketWorkspaceLinkCode(
		context.Context,
		*controltowerv1.CreateBitbucketWorkspaceLinkCodeRequest,
		...grpc.CallOption,
	) (*controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse, error)
}

type linkDeleter interface {
	DeleteBitbucketWorkspaceLink(
		context.Context,
		*controltowerv1.DeleteBitbucketWorkspaceLinkRequest,
		...grpc.CallOption,
	) (*controltowerv1.DeleteBitbucketWorkspaceLinkResponse, error)
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

// addLinkFlags wires the workspace selection every link-scoped command
// takes. --link-id is hidden: people name a workspace by its slug, and the
// flag stays for scripts that pass the internal link ID.
func addLinkFlags(cmd *cobra.Command, account, linkID *string, verb string) {
	f := cmd.Flags()
	f.StringVar(account, "account", "",
		"Bitbucket workspace slug of the link "+verb+". Needed when the tenant has more than one linked workspace.")
	f.StringVar(linkID, "link-id", "", "Bitbucket workspace link ID "+verb)
	f.Lookup("link-id").Hidden = true
}
