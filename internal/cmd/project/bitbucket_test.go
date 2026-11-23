package project

import (
	"context"
	"errors"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/bitbucketlink"
)

type fakeBitbucketLinkLister struct {
	pages []*controltowerv1.ListBitbucketWorkspaceLinksResponse
	calls int
	err   error
}

func (f *fakeBitbucketLinkLister) ListBitbucketWorkspaceLinks(
	_ context.Context,
	_ *controltowerv1.ListBitbucketWorkspaceLinksRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListBitbucketWorkspaceLinksResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) == 0 {
		return nil, errors.New("unexpected ListBitbucketWorkspaceLinks call")
	}
	return f.pages[min(f.calls-1, len(f.pages)-1)], nil
}

type fakeBitbucketRepositoryLister struct {
	pages    []*controltowerv1.ListBitbucketRepositoriesResponse
	calls    int
	requests []*controltowerv1.ListBitbucketRepositoriesRequest
	err      error
}

func (f *fakeBitbucketRepositoryLister) ListBitbucketRepositories(
	_ context.Context,
	req *controltowerv1.ListBitbucketRepositoriesRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListBitbucketRepositoriesResponse, error) {
	f.calls++
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) == 0 {
		return nil, errors.New("unexpected ListBitbucketRepositories call")
	}
	return f.pages[min(f.calls-1, len(f.pages)-1)], nil
}

// fakeBitbucketProjectSyncer answers each request with one project per
// requested repository, unless res fixes the response.
type fakeBitbucketProjectSyncer struct {
	calls int
	req   *controltowerv1.SyncBitbucketProjectsRequest
	reqs  []*controltowerv1.SyncBitbucketProjectsRequest
	res   *controltowerv1.SyncBitbucketProjectsResponse
	err   error
}

func (f *fakeBitbucketProjectSyncer) SyncBitbucketProjects(
	_ context.Context,
	req *controltowerv1.SyncBitbucketProjectsRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.SyncBitbucketProjectsResponse, error) {
	f.calls++
	f.req = req
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return nil, f.err
	}
	if f.res != nil {
		return f.res, nil
	}
	mappings := make([]*controltowerv1.SyncBitbucketProjectsResponse_ProjectMapping, 0, len(req.GetRepositories()))
	for _, selection := range req.GetRepositories() {
		mappings = append(mappings, bitbucketProjectMapping(selection.GetRepositoryUuid(),
			"project-"+selection.GetRepositoryUuid()))
	}
	return bitbucketSyncResponse(mappings...), nil
}

func syncedUUIDs(req *controltowerv1.SyncBitbucketProjectsRequest) []string {
	uuids := make([]string, 0, len(req.GetRepositories()))
	for _, selection := range req.GetRepositories() {
		uuids = append(uuids, selection.GetRepositoryUuid())
	}
	return uuids
}

var (
	_ bitbucketlink.Lister      = (*fakeBitbucketLinkLister)(nil)
	_ bitbucketRepositoryLister = (*fakeBitbucketRepositoryLister)(nil)
	_ bitbucketProjectSyncer    = (*fakeBitbucketProjectSyncer)(nil)
)

func bitbucketLinksPage(links ...*controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink) *controltowerv1.ListBitbucketWorkspaceLinksResponse {
	res := &controltowerv1.ListBitbucketWorkspaceLinksResponse{}
	res.SetLinks(links)
	return res
}

func bitbucketWorkspaceLink(linkID string) *controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink {
	return bitbucketWorkspaceLinkOf(linkID, "safedep")
}

func bitbucketWorkspaceLinkOf(linkID, slug string) *controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink {
	link := &controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink{}
	link.SetLinkId(linkID)
	link.SetWorkspaceSlug(slug)
	return link
}

func bitbucketRepositoriesPage(repositories ...*controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository) *controltowerv1.ListBitbucketRepositoriesResponse {
	res := &controltowerv1.ListBitbucketRepositoriesResponse{}
	res.SetRepositories(repositories)
	return res
}

func bitbucketListedRepository(uuid, fullName string) *controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository {
	repository := &controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository{}
	repository.SetRepositoryUuid(uuid)
	repository.SetFullName(fullName)
	return repository
}

func bitbucketSyncResponse(mappings ...*controltowerv1.SyncBitbucketProjectsResponse_ProjectMapping) *controltowerv1.SyncBitbucketProjectsResponse {
	res := &controltowerv1.SyncBitbucketProjectsResponse{}
	res.SetProjects(mappings)
	return res
}

func bitbucketProjectMapping(uuid, projectID string) *controltowerv1.SyncBitbucketProjectsResponse_ProjectMapping {
	mapping := &controltowerv1.SyncBitbucketProjectsResponse_ProjectMapping{}
	mapping.SetRepositoryUuid(uuid)
	mapping.SetProjectId(projectID)
	return mapping
}

const (
	testBitbucketRepoUUIDa = "d6c2f02b-7f78-49a8-b7e3-cf8b86efd115"
	testBitbucketRepoUUIDb = "aaaa0000-1111-2222-3333-444455556666"
)

func TestRunSync_Bitbucket(t *testing.T) {
	t.Run("resolves names to UUIDs and syncs through the only link", func(t *testing.T) {
		githubLinks := &fakeGitHubLinkLister{}
		links := &fakeBitbucketLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			bitbucketLinksPage(bitbucketWorkspaceLink("link-1")),
		}}
		repositories := &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(
				bitbucketListedRepository(testBitbucketRepoUUIDa, "safedep/vet-pipe"),
			),
		}}
		syncer := &fakeBitbucketProjectSyncer{}

		got, err := runSync(context.Background(), syncDeps{
			githubLinks: githubLinks, bitbucketLinks: links, bitbucketRepositories: repositories, bitbucketSyncer: syncer,
		}, syncInput{Source: "bitbucket", RepositoryNames: []string{"SafeDep/Vet-Pipe"}})
		require.NoError(t, err)
		assert.Zero(t, githubLinks.calls, "--source bitbucket lists no GitHub links")
		require.Equal(t, 1, syncer.calls)
		assert.Equal(t, "link-1", syncer.req.GetLinkId())
		assert.Equal(t, []string{testBitbucketRepoUUIDa}, syncedUUIDs(syncer.req))
		result, ok := got.(*bitbucketSyncResult)
		require.True(t, ok)
		require.Len(t, result.projects, 1)
		assert.Equal(t, syncedBitbucketProject{
			RepositoryUUID: testBitbucketRepoUUIDa,
			RepositoryName: "safedep/vet-pipe",
			ProjectID:      "project-" + testBitbucketRepoUUIDa,
			Account:        "safedep",
			LinkID:         "link-1",
		}, result.projects[0])
	})

	t.Run("UUID selectors skip name resolution", func(t *testing.T) {
		repositories := &fakeBitbucketRepositoryLister{}
		syncer := &fakeBitbucketProjectSyncer{}

		got, err := runSync(context.Background(), syncDeps{bitbucketRepositories: repositories, bitbucketSyncer: syncer},
			syncInput{LinkID: "link-9", RepositoryUUIDs: []string{testBitbucketRepoUUIDb}})
		require.NoError(t, err)
		assert.Equal(t, 0, repositories.calls)
		assert.Equal(t, "link-9", syncer.req.GetLinkId())
		result, ok := got.(*bitbucketSyncResult)
		require.True(t, ok)
		assert.Contains(t, result.RenderPlain(), "-\t"+testBitbucketRepoUUIDb)
	})

	t.Run("an unreachable name fails without a sync", func(t *testing.T) {
		repositories := &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(),
		}}
		syncer := &fakeBitbucketProjectSyncer{}

		_, err := runSync(context.Background(), syncDeps{bitbucketRepositories: repositories, bitbucketSyncer: syncer},
			syncInput{Source: "bitbucket", LinkID: "link-1", RepositoryNames: []string{"safedep/ghost"}})
		require.ErrorContains(t, err, "not accessible through workspace link")
		assert.Equal(t, 0, syncer.calls)
	})

	t.Run("a name that resolves to a selected UUID is rejected", func(t *testing.T) {
		repositories := &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(
				bitbucketListedRepository(testBitbucketRepoUUIDa, "safedep/vet-pipe"),
			),
		}}
		syncer := &fakeBitbucketProjectSyncer{}

		_, err := runSync(context.Background(), syncDeps{bitbucketRepositories: repositories, bitbucketSyncer: syncer},
			syncInput{
				LinkID:          "link-1",
				RepositoryNames: []string{"safedep/vet-pipe"},
				RepositoryUUIDs: []string{testBitbucketRepoUUIDa},
			})
		require.ErrorContains(t, err, "duplicate repository UUID")
		assert.Equal(t, 0, syncer.calls)
	})

	t.Run("rejects a response that does not cover the request", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse()}

		_, err := runSync(context.Background(), syncDeps{bitbucketSyncer: syncer},
			syncInput{LinkID: "link-1", RepositoryUUIDs: []string{testBitbucketRepoUUIDa}})
		require.ErrorContains(t, err, "response count 0 does not match request count 1")
	})

	t.Run("rejects a mapping without a project ID", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse(
			bitbucketProjectMapping(testBitbucketRepoUUIDa, ""),
		)}

		_, err := runSync(context.Background(), syncDeps{bitbucketSyncer: syncer},
			syncInput{LinkID: "link-1", RepositoryUUIDs: []string{testBitbucketRepoUUIDa}})
		require.ErrorContains(t, err, "missing its project ID")
	})
}

func TestRunSync_NormalizesRepositoryUUIDs(t *testing.T) {
	t.Parallel()

	t.Run("a braced uppercase UUID normalizes before the request", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse(
			bitbucketProjectMapping(testBitbucketRepoUUIDa, "project-1"),
		)}

		_, err := runSync(context.Background(), syncDeps{bitbucketSyncer: syncer}, syncInput{
			LinkID:          "link-1",
			RepositoryUUIDs: []string{"{D6C2F02B-7F78-49A8-B7E3-CF8B86EFD115}"},
		})
		require.NoError(t, err)
		require.Len(t, syncer.req.GetRepositories(), 1)
		assert.Equal(t, testBitbucketRepoUUIDa, syncer.req.GetRepositories()[0].GetRepositoryUuid())
	})

	t.Run("a malformed UUID fails before any RPC", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{}

		_, err := runSync(context.Background(), syncDeps{bitbucketSyncer: syncer}, syncInput{
			RepositoryUUIDs: []string{"not-a-uuid"},
		})
		require.ErrorContains(t, err, `invalid repository UUID "not-a-uuid"`)
		assert.Equal(t, 0, syncer.calls)
	})
}

func TestRunSync_ListsEachSourceOnceForANamesOnlySync(t *testing.T) {
	t.Parallel()

	githubLinks := &fakeGitHubLinkLister{pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{
		linksResponse("", link("gh-link-1", "safedep")),
	}}
	bitbucketLinks := &fakeBitbucketLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
		bitbucketLinksPage(),
	}}
	repositories := &fakeGitHubRepositoryLister{
		pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("", repository(12, "safedep/cli")),
		},
	}
	syncer := &fakeGitHubProjectSyncer{res: syncResponse(projectMapping(12, "project-12"))}

	deps := syncDeps{
		githubLinks:        githubLinks,
		githubRepositories: repositories,
		githubSyncer:       syncer,
		bitbucketLinks:     bitbucketLinks,
	}
	_, err := runSync(context.Background(), deps, syncInput{
		RepositoryNames: []string{"safedep/cli"},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, githubLinks.calls, "the inference walk must also resolve the link")
	assert.Equal(t, 1, bitbucketLinks.calls)
	assert.Equal(t, "gh-link-1", syncer.req.GetLinkId())
}
