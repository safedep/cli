package project

import (
	"context"
	"errors"
	"testing"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeGitHubLinkLister struct {
	pages []*controltowerv1.ListGitHubAppInstallationLinksResponse
	calls int
	err   error
}

func (f *fakeGitHubLinkLister) ListGitHubAppInstallationLinks(
	_ context.Context,
	_ *controltowerv1.ListGitHubAppInstallationLinksRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListGitHubAppInstallationLinksResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) == 0 {
		return nil, errors.New("unexpected ListGitHubAppInstallationLinks call")
	}
	return f.pages[min(f.calls-1, len(f.pages)-1)], nil
}

type fakeGitHubRepositoryLister struct {
	pages    []*controltowerv1.ListGitHubInstallationRepositoriesResponse
	requests []*controltowerv1.ListGitHubInstallationRepositoriesRequest
	err      error
}

func (f *fakeGitHubRepositoryLister) ListGitHubInstallationRepositories(
	_ context.Context,
	req *controltowerv1.ListGitHubInstallationRepositoriesRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListGitHubInstallationRepositoriesResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) == 0 {
		return nil, errors.New("unexpected ListGitHubInstallationRepositories call")
	}
	return f.pages[min(len(f.requests)-1, len(f.pages)-1)], nil
}

type fakeGitHubProjectSyncer struct {
	calls int
	req   *controltowerv1.SyncGitHubInstallationProjectsRequest
	res   *controltowerv1.SyncGitHubInstallationProjectsResponse
	err   error
}

func (f *fakeGitHubProjectSyncer) SyncGitHubInstallationProjects(
	_ context.Context,
	req *controltowerv1.SyncGitHubInstallationProjectsRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.SyncGitHubInstallationProjectsResponse, error) {
	f.calls++
	f.req = req
	return f.res, f.err
}

var (
	_ githubLinkLister       = (*fakeGitHubLinkLister)(nil)
	_ githubRepositoryLister = (*fakeGitHubRepositoryLister)(nil)
	_ githubProjectSyncer    = (*fakeGitHubProjectSyncer)(nil)

	_ githubLinkLister       = controltowerv1grpc.IntegrationServiceClient(nil)
	_ githubRepositoryLister = controltowerv1grpc.IntegrationServiceClient(nil)
	_ githubProjectSyncer    = controltowerv1grpc.IntegrationServiceClient(nil)
)

func TestRunSync_ResolvesNamesAndSendsFlagIDsFirst(t *testing.T) {
	t.Parallel()

	links := &fakeGitHubLinkLister{pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{
		linksResponse("", link("link-1", "safedep")),
	}}
	repositories := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("", repository(11, "safedep/vet"), repository(12, "safedep/cli")),
		},
	}
	syncer := &fakeGitHubProjectSyncer{res: syncResponse(
		projectMapping(99, "project-99"),
		projectMapping(12, "project-12"),
		projectMapping(11, "project-11"),
	)}

	result, err := runSync(context.Background(), links, repositories, syncer, syncInput{
		RepositoryNames: []string{"safedep/cli", "safedep/vet"},
		RepositoryIDs:   []int64{99},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, links.calls, "the link must be resolved once")
	require.Len(t, repositories.requests, 1)
	assert.Equal(t, "link-1", repositories.requests[0].GetLinkId())

	require.Equal(t, 1, syncer.calls)
	assert.Equal(t, "link-1", syncer.req.GetLinkId())
	require.Len(t, syncer.req.GetRepositories(), 3)
	assert.Equal(t, int64(99), syncer.req.GetRepositories()[0].GetRepositoryId())
	assert.Equal(t, int64(12), syncer.req.GetRepositories()[1].GetRepositoryId())
	assert.Equal(t, int64(11), syncer.req.GetRepositories()[2].GetRepositoryId())

	assert.Equal(t, "link-1", result.linkID)
	require.Len(t, result.projects, 3)
	assert.Equal(t, syncedProject{RepositoryID: 99, ProjectID: "project-99"}, result.projects[0])
	assert.Equal(t, syncedProject{
		RepositoryID: 12, RepositoryName: "safedep/cli", ProjectID: "project-12",
	}, result.projects[1])
	assert.Equal(t, syncedProject{
		RepositoryID: 11, RepositoryName: "safedep/vet", ProjectID: "project-11",
	}, result.projects[2])
}

func TestRunSync_SkipsLinkResolutionWhenLinkIDIsSupplied(t *testing.T) {
	t.Parallel()

	links := &fakeGitHubLinkLister{}
	repositories := &fakeGitHubRepositoryLister{}
	syncer := &fakeGitHubProjectSyncer{res: syncResponse(projectMapping(99, "project-99"))}

	result, err := runSync(context.Background(), links, repositories, syncer, syncInput{
		LinkID:        "link-explicit",
		RepositoryIDs: []int64{99},
	})
	require.NoError(t, err)

	assert.Zero(t, links.calls)
	assert.Empty(t, repositories.requests, "no name needs resolving")
	assert.Equal(t, "link-explicit", syncer.req.GetLinkId())
	assert.Equal(t, "link-explicit", result.linkID)
}

func TestRunSync_MatchesRepositoryNamesCaseInsensitively(t *testing.T) {
	t.Parallel()

	repositories := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("", repository(12, "SafeDep/CLI")),
		},
	}
	syncer := &fakeGitHubProjectSyncer{res: syncResponse(projectMapping(12, "project-12"))}

	result, err := runSync(context.Background(), &fakeGitHubLinkLister{}, repositories, syncer, syncInput{
		LinkID:          "link-1",
		RepositoryNames: []string{"safedep/cli"},
	})
	require.NoError(t, err)

	require.Len(t, result.projects, 1)
	assert.Equal(t, "SafeDep/CLI", result.projects[0].RepositoryName,
		"GitHub's spelling wins over the caller's")
}

func TestRunSync_RejectsANameThatCollidesWithASelectedID(t *testing.T) {
	t.Parallel()

	repositories := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("", repository(12, "safedep/cli")),
		},
	}
	syncer := &fakeGitHubProjectSyncer{}

	_, err := runSync(context.Background(), &fakeGitHubLinkLister{}, repositories, syncer, syncInput{
		LinkID:          "link-1",
		RepositoryNames: []string{"safedep/cli"},
		RepositoryIDs:   []int64{12},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate repository ID 12")
	assert.Zero(t, syncer.calls, "nothing must be materialized")
}

func TestRunSync_RejectsInvalidInputBeforeAnyRPC(t *testing.T) {
	t.Parallel()

	links := &fakeGitHubLinkLister{}
	repositories := &fakeGitHubRepositoryLister{}
	syncer := &fakeGitHubProjectSyncer{}

	_, err := runSync(context.Background(), links, repositories, syncer, syncInput{})
	require.Error(t, err)
	assert.Zero(t, links.calls)
	assert.Empty(t, repositories.requests)
	assert.Zero(t, syncer.calls)
}

func TestResolveGitHubLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pages   []*controltowerv1.ListGitHubAppInstallationLinksResponse
		want    string
		wantErr string
	}{
		{
			name:  "exactly one link",
			pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{linksResponse("", link("link-1", "safedep"))},
			want:  "link-1",
		},
		{
			name:    "no link",
			pages:   []*controltowerv1.ListGitHubAppInstallationLinksResponse{linksResponse("")},
			wantErr: "no GitHub App installation link",
		},
		{
			name: "several links",
			pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{
				linksResponse("", link("link-2", "acme"), link("link-1", "")),
			},
			wantErr: "link-1, link-2 (acme)",
		},
		{
			name: "several links across pages",
			pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{
				linksResponse("page-2", link("link-1", "safedep")),
				linksResponse("", link("link-2", "acme")),
			},
			wantErr: "link-1 (safedep), link-2 (acme)",
		},
		{
			name:    "missing link ID",
			pages:   []*controltowerv1.ListGitHubAppInstallationLinksResponse{linksResponse("", link("", "safedep"))},
			wantErr: "link 1 is missing its ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveGitHubLink(context.Background(), &fakeGitHubLinkLister{pages: tt.pages})
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestResolveGitHubLink_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	client := &fakeGitHubLinkLister{err: status.Error(codes.Unavailable, "github unavailable")}
	_, err := resolveGitHubLink(context.Background(), client)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestResolveRepositoryNames_StopsPagingOnceEveryNameIsMatched(t *testing.T) {
	t.Parallel()

	client := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("2", repository(11, "safedep/vet")),
			repositoriesResponse("3", repository(12, "safedep/cli")),
			repositoriesResponse("", repository(13, "safedep/pmg")),
		},
	}

	resolved, err := resolveRepositoryNames(
		context.Background(), client, "link-1", []string{"safedep/cli", "safedep/vet"},
	)
	require.NoError(t, err)

	require.Len(t, resolved, 2)
	assert.Equal(t, githubRepository{id: 12, fullName: "safedep/cli"}, resolved[0])
	assert.Equal(t, githubRepository{id: 11, fullName: "safedep/vet"}, resolved[1])
	assert.Len(t, client.requests, 2, "the third page must not be fetched")
	assert.Equal(t, "2", client.requests[1].GetPagination().GetPageToken())
	assert.Equal(t, uint32(githubRepositoryPageSize), client.requests[0].GetPagination().GetPageSize())
}

func TestResolveRepositoryNames_NoRequestWithoutNames(t *testing.T) {
	t.Parallel()

	client := &fakeGitHubRepositoryLister{}
	resolved, err := resolveRepositoryNames(context.Background(), client, "link-1", nil)
	require.NoError(t, err)
	assert.Empty(t, resolved)
	assert.Empty(t, client.requests)
}

func TestResolveRepositoryNames_FailsOnAnInaccessibleRepository(t *testing.T) {
	t.Parallel()

	client := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("", repository(11, "safedep/vet")),
		},
	}

	_, err := resolveRepositoryNames(
		context.Background(), client, "link-1", []string{"safedep/vet", "safedep/secret"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `repository "safedep/secret" is not accessible`)
	assert.Contains(t, err.Error(), "link-1")
}

func TestResolveRepositoryNames_RejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repository *controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository
	}{
		{name: "missing ID", repository: repository(0, "safedep/cli")},
		{name: "negative ID", repository: repository(-1, "safedep/cli")},
		{name: "missing full name", repository: repository(11, "")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeGitHubRepositoryLister{
				pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
					repositoriesResponse("", tt.repository),
				},
			}
			_, err := resolveRepositoryNames(context.Background(), client, "link-1", []string{"safedep/cli"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "repository 1 is missing its identity")
		})
	}
}

func TestResolveRepositoryNames_StopsOnARepeatedPageToken(t *testing.T) {
	t.Parallel()

	client := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("2", repository(11, "safedep/vet")),
		},
	}

	_, err := resolveRepositoryNames(context.Background(), client, "link-1", []string{"safedep/cli"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeated page token")
	assert.Len(t, client.requests, 2)
}

func TestSyncGitHubProjects_RejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		res     *controltowerv1.SyncGitHubInstallationProjectsResponse
		wantErr string
	}{
		{name: "wrong count", res: syncResponse(), wantErr: "response count"},
		{
			name:    "unexpected repository",
			res:     syncResponse(projectMapping(11, "project-11"), projectMapping(13, "project-13")),
			wantErr: "unexpected repository ID 13",
		},
		{
			name:    "duplicate repository",
			res:     syncResponse(projectMapping(11, "project-11"), projectMapping(11, "project-11")),
			wantErr: "duplicate repository ID 11",
		},
		{
			name:    "missing project ID",
			res:     syncResponse(projectMapping(11, "project-11"), projectMapping(12, "")),
			wantErr: "repository ID 12 is missing its project ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			syncer := &fakeGitHubProjectSyncer{res: tt.res}
			_, err := syncGitHubProjects(
				context.Background(), syncer, "link-1", []int64{11, 12}, map[int64]string{},
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "project sync: invalid response")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSyncGitHubProjects_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	syncer := &fakeGitHubProjectSyncer{err: status.Error(codes.FailedPrecondition, "installation suspended")}
	_, err := syncGitHubProjects(context.Background(), syncer, "link-1", []int64{11}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project sync")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func linksResponse(
	nextPageToken string,
	links ...*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes,
) *controltowerv1.ListGitHubAppInstallationLinksResponse {
	res := &controltowerv1.ListGitHubAppInstallationLinksResponse{}
	res.SetIntegrations(links)
	if nextPageToken != "" {
		pagination := &messagescontroltowerv1.PaginationResponse{}
		pagination.SetNextPageToken(nextPageToken)
		res.SetPagination(pagination)
	}
	return res
}

func link(
	linkID string,
	accountLogin string,
) *controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes {
	item := &controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes{}
	item.SetLinkId(linkID)
	item.SetAccountLogin(accountLogin)
	return item
}

func repositoriesResponse(
	nextPageToken string,
	repositories ...*controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository,
) *controltowerv1.ListGitHubInstallationRepositoriesResponse {
	res := &controltowerv1.ListGitHubInstallationRepositoriesResponse{}
	res.SetRepositories(repositories)
	if nextPageToken != "" {
		pagination := &messagescontroltowerv1.PaginationResponse{}
		pagination.SetNextPageToken(nextPageToken)
		res.SetPagination(pagination)
	}
	return res
}

func repository(
	repositoryID int64,
	fullName string,
) *controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository {
	repo := &controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository{}
	repo.SetRepositoryId(repositoryID)
	repo.SetFullName(fullName)
	return repo
}

func syncResponse(
	mappings ...*controltowerv1.SyncGitHubInstallationProjectsResponse_ProjectMapping,
) *controltowerv1.SyncGitHubInstallationProjectsResponse {
	res := &controltowerv1.SyncGitHubInstallationProjectsResponse{}
	res.SetProjects(mappings)
	return res
}

func projectMapping(
	repositoryID int64,
	projectID string,
) *controltowerv1.SyncGitHubInstallationProjectsResponse_ProjectMapping {
	mapping := &controltowerv1.SyncGitHubInstallationProjectsResponse_ProjectMapping{}
	mapping.SetRepositoryId(repositoryID)
	mapping.SetProjectId(projectID)
	return mapping
}
