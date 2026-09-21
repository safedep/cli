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
	pages []*controltowerv1.ListBitbucketRepositoriesResponse
	calls int
	err   error
}

func (f *fakeBitbucketRepositoryLister) ListBitbucketRepositories(
	_ context.Context,
	_ *controltowerv1.ListBitbucketRepositoriesRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListBitbucketRepositoriesResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) == 0 {
		return nil, errors.New("unexpected ListBitbucketRepositories call")
	}
	return f.pages[min(f.calls-1, len(f.pages)-1)], nil
}

type fakeBitbucketProjectSyncer struct {
	calls int
	req   *controltowerv1.SyncBitbucketProjectsRequest
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
	return f.res, f.err
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
	link := &controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink{}
	link.SetLinkId(linkID)
	link.SetWorkspaceSlug("safedep")
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

func TestRunBitbucketSync(t *testing.T) {
	t.Run("resolves names to UUIDs and syncs through the only link", func(t *testing.T) {
		links := &fakeBitbucketLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			bitbucketLinksPage(bitbucketWorkspaceLink("link-1")),
		}}
		repositories := &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(
				bitbucketListedRepository(testBitbucketRepoUUIDa, "safedep/vet-pipe"),
			),
		}}
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse(
			bitbucketProjectMapping(testBitbucketRepoUUIDa, "project-1"),
		)}

		result, err := runBitbucketSync(context.Background(), links, repositories, syncer,
			syncInput{RepositoryNames: []string{"SafeDep/Vet-Pipe"}})
		require.NoError(t, err)
		require.Equal(t, 1, syncer.calls)
		assert.Equal(t, "link-1", syncer.req.GetLinkId())
		require.Len(t, syncer.req.GetRepositories(), 1)
		assert.Equal(t, testBitbucketRepoUUIDa, syncer.req.GetRepositories()[0].GetRepositoryUuid())
		require.Len(t, result.projects, 1)
		assert.Equal(t, syncedBitbucketProject{
			RepositoryUUID: testBitbucketRepoUUIDa,
			RepositoryName: "safedep/vet-pipe",
			ProjectID:      "project-1",
		}, result.projects[0])
	})

	t.Run("UUID selectors skip name resolution", func(t *testing.T) {
		repositories := &fakeBitbucketRepositoryLister{}
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse(
			bitbucketProjectMapping(testBitbucketRepoUUIDb, "project-2"),
		)}

		result, err := runBitbucketSync(context.Background(), &fakeBitbucketLinkLister{},
			repositories, syncer, syncInput{
				LinkID:          "link-9",
				RepositoryUUIDs: []string{testBitbucketRepoUUIDb},
			})
		require.NoError(t, err)
		assert.Equal(t, 0, repositories.calls)
		assert.Equal(t, "link-9", syncer.req.GetLinkId())
		require.Len(t, result.projects, 1)
		assert.Equal(t, unknownRepositoryName, syncedBitbucketProjectCells(result.projects[0])[0])
	})

	t.Run("an unreachable name fails without a sync", func(t *testing.T) {
		repositories := &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(),
		}}
		syncer := &fakeBitbucketProjectSyncer{}

		_, err := runBitbucketSync(context.Background(), &fakeBitbucketLinkLister{},
			repositories, syncer, syncInput{
				LinkID:          "link-1",
				RepositoryNames: []string{"safedep/ghost"},
			})
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

		_, err := runBitbucketSync(context.Background(), &fakeBitbucketLinkLister{},
			repositories, syncer, syncInput{
				LinkID:          "link-1",
				RepositoryNames: []string{"safedep/vet-pipe"},
				RepositoryUUIDs: []string{testBitbucketRepoUUIDa},
			})
		require.ErrorContains(t, err, "duplicate repository UUID")
		assert.Equal(t, 0, syncer.calls)
	})

	t.Run("rejects a response that does not cover the request", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse()}

		_, err := runBitbucketSync(context.Background(), &fakeBitbucketLinkLister{},
			&fakeBitbucketRepositoryLister{}, syncer, syncInput{
				LinkID:          "link-1",
				RepositoryUUIDs: []string{testBitbucketRepoUUIDa},
			})
		require.ErrorContains(t, err, "response count 0 does not match request count 1")
	})

	t.Run("rejects a mapping without a project ID", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{res: bitbucketSyncResponse(
			bitbucketProjectMapping(testBitbucketRepoUUIDa, ""),
		)}

		_, err := runBitbucketSync(context.Background(), &fakeBitbucketLinkLister{},
			&fakeBitbucketRepositoryLister{}, syncer, syncInput{
				LinkID:          "link-1",
				RepositoryUUIDs: []string{testBitbucketRepoUUIDa},
			})
		require.ErrorContains(t, err, "missing its project ID")
	})
}

func TestResolveSyncSource(t *testing.T) {
	githubLinksPage := func(linkIDs ...string) *controltowerv1.ListGitHubAppInstallationLinksResponse {
		links := make([]*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes, 0, len(linkIDs))
		for _, linkID := range linkIDs {
			links = append(links, link(linkID, "safedep"))
		}
		return linksResponse("", links...)
	}

	cases := []struct {
		name       string
		in         syncInput
		github     []*controltowerv1.ListGitHubAppInstallationLinksResponse
		bitbucket  []*controltowerv1.ListBitbucketWorkspaceLinksResponse
		wantSource string
		wantErr    string
	}{
		{
			name:       "explicit source wins",
			in:         syncInput{Source: sourceBitbucket},
			wantSource: sourceBitbucket,
		},
		{
			name:       "repository IDs imply github",
			in:         syncInput{RepositoryIDs: []int64{7}},
			wantSource: sourceGitHub,
		},
		{
			name:       "repository UUIDs imply bitbucket",
			in:         syncInput{RepositoryUUIDs: []string{testBitbucketRepoUUIDa}},
			wantSource: sourceBitbucket,
		},
		{
			name:       "a bitbucket-only tenant infers bitbucket",
			github:     []*controltowerv1.ListGitHubAppInstallationLinksResponse{githubLinksPage()},
			bitbucket:  []*controltowerv1.ListBitbucketWorkspaceLinksResponse{bitbucketLinksPage(bitbucketWorkspaceLink("link-1"))},
			wantSource: sourceBitbucket,
		},
		{
			name:       "a github-only tenant infers github",
			github:     []*controltowerv1.ListGitHubAppInstallationLinksResponse{githubLinksPage("gh-link-1")},
			bitbucket:  []*controltowerv1.ListBitbucketWorkspaceLinksResponse{bitbucketLinksPage()},
			wantSource: sourceGitHub,
		},
		{
			name:      "links to both sources need an explicit source",
			github:    []*controltowerv1.ListGitHubAppInstallationLinksResponse{githubLinksPage("gh-link-1")},
			bitbucket: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{bitbucketLinksPage(bitbucketWorkspaceLink("link-1"))},
			wantErr:   "has GitHub and Bitbucket links",
		},
		{
			name:      "no links at all is a not-found failure",
			github:    []*controltowerv1.ListGitHubAppInstallationLinksResponse{githubLinksPage()},
			bitbucket: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{bitbucketLinksPage()},
			wantErr:   "no GitHub or Bitbucket link",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := syncDeps{
				githubLinks:    &fakeGitHubLinkLister{pages: tc.github},
				bitbucketLinks: &fakeBitbucketLinkLister{pages: tc.bitbucket},
			}

			source, err := resolveSyncSource(context.Background(), deps, tc.in)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantSource, source)
		})
	}
}
