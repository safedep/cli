package project

import (
	"context"
	"fmt"
	"strings"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// githubRepositoryRange lists repositories first..last as owner safedep.
func githubRepositoryRange(first, last int64) []*controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository {
	var out []*controltowerv1.ListGitHubInstallationRepositoriesResponse_GitHubRepository
	for id := first; id <= last; id++ {
		out = append(out, repository(id, fmt.Sprintf("safedep/repo-%d", id)))
	}
	return out
}

func TestRunSync_AllGitHub(t *testing.T) {
	linksPage := linksResponse("", link("gh-1", "safedep"), link("gh-2", "acme"))

	t.Run("syncs every listed repository in batches of 100", func(t *testing.T) {
		// Pages of 60, 100 and 80 repositories. Page two repeats repository
		// 60, which the sync must count once.
		page2 := append(githubRepositoryRange(61, 160), repository(60, "safedep/repo-60"))
		repositories := &fakeGitHubRepositoryLister{pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("p2", githubRepositoryRange(1, 60)...),
			repositoriesResponse("p3", page2...),
			repositoriesResponse("", githubRepositoryRange(161, 240)...),
		}}
		syncer := &fakeGitHubProjectSyncer{}
		var progress []string

		got, err := runSync(context.Background(), syncDeps{
			progress:           func(format string, args ...any) { progress = append(progress, fmt.Sprintf(format, args...)) },
			githubLinks:        &fakeGitHubLinkLister{pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{linksPage}},
			githubRepositories: repositories,
			githubSyncer:       syncer,
		}, syncInput{Source: "github", Account: "Acme", All: true})
		require.NoError(t, err)

		require.Len(t, repositories.requests, 3)
		assert.Equal(t, "gh-2", repositories.requests[0].GetLinkId())
		assert.Equal(t, "p3", repositories.requests[2].GetPagination().GetPageToken())

		require.Len(t, syncer.reqs, 3)
		sizes := make([]int, 0, len(syncer.reqs))
		for _, req := range syncer.reqs {
			assert.Equal(t, "gh-2", req.GetLinkId())
			sizes = append(sizes, len(req.GetRepositories()))
		}
		assert.Equal(t, []int{100, 100, 40}, sizes)
		assert.Equal(t, []string{
			"Syncing repositories 1 to 100 of 240",
			"Syncing repositories 101 to 200 of 240",
			"Syncing repositories 201 to 240 of 240",
		}, progress)

		result, ok := got.(*syncResult)
		require.True(t, ok)
		assert.Len(t, result.projects, 240)
		assert.Equal(t, "safedep/repo-240", result.projects[239].RepositoryName)
		assert.Contains(t, result.RenderTable(), "Synced 240 repositories through account acme")
	})

	t.Run("an installation without repositories syncs nothing", func(t *testing.T) {
		syncer := &fakeGitHubProjectSyncer{}

		got, err := runSync(context.Background(), syncDeps{
			githubLinks: &fakeGitHubLinkLister{pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{linksPage}},
			githubRepositories: &fakeGitHubRepositoryLister{pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
				repositoriesResponse(""),
			}},
			githubSyncer: syncer,
		}, syncInput{Source: "github", Account: "acme", All: true})
		require.NoError(t, err)
		assert.Zero(t, syncer.calls)
		result, ok := got.(*syncResult)
		require.True(t, ok)
		assert.Contains(t, result.RenderTable(), "Synced 0 repositories through account acme")
	})

	failures := []struct {
		name      string
		err       error
		wantCode  string
		wantRerun bool
	}{
		{
			name:     "a refusal stops the run without rerun advice",
			err:      status.Error(codes.FailedPrecondition, "installation suspended"),
			wantCode: usefulerror.ErrConflict,
		},
		{
			name:      "an outage stops the run with rerun advice",
			err:       status.Error(codes.Unavailable, "control plane unavailable"),
			wantCode:  usefulerror.ErrServiceUnavailable,
			wantRerun: true,
		},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			syncer := &fakeGitHubProjectSyncer{err: tc.err, failOnCall: 2}

			_, err := runSync(context.Background(), syncDeps{
				githubLinks: &fakeGitHubLinkLister{pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{linksPage}},
				githubRepositories: &fakeGitHubRepositoryLister{pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
					repositoriesResponse("", githubRepositoryRange(1, 240)...),
				}},
				githubSyncer: syncer,
			}, syncInput{Source: "github", Account: "acme", All: true})
			require.Error(t, err)
			assert.Equal(t, 2, syncer.calls, "the batch after the failure must not be sent")

			usefulErr, ok := usefulerror.AsUsefulError(err)
			require.True(t, ok)
			assert.Equal(t, "Sync stopped after 100 of 240 repositories", usefulErr.HumanError())
			assert.Equal(t, tc.wantCode, usefulErr.Code())
			assert.Contains(t, usefulErr.Help(), status.Convert(tc.err).Message())
			assert.Equal(t, tc.wantRerun, strings.Contains(usefulErr.Help(), "Rerun the same command"))
			assert.Equal(t, status.Code(tc.err), status.Code(err))
		})
	}
}

func TestRunSync_AllBitbucket(t *testing.T) {
	const (
		enabled  = controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_ENABLED
		disabled = controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_DISABLED
	)
	listed := func(uuid string, state controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_ScanState) *controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository {
		repository := bitbucketListedRepository(uuid, "safedep/"+uuid[:4])
		repository.SetScanState(state)
		return repository
	}

	t.Run("syncs only repositories with scan state enabled", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{}
		got, err := runSync(context.Background(), syncDeps{
			bitbucketLinks: &fakeBitbucketLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
				bitbucketLinksPage(bitbucketWorkspaceLink("bb-1")),
			}},
			bitbucketRepositories: &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
				bitbucketRepositoriesPage(
					listed(testBitbucketRepoUUIDa, enabled),
					listed("11111111-0000-4000-8000-000000000001", disabled),
					listed(testBitbucketRepoUUIDb, enabled),
					listed("11111111-0000-4000-8000-000000000002",
						controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_UNSPECIFIED),
				),
			}},
			bitbucketSyncer: syncer,
		}, syncInput{Source: "bitbucket", Account: "safedep", All: true})
		require.NoError(t, err)
		require.Equal(t, 1, syncer.calls)
		assert.Equal(t, []string{testBitbucketRepoUUIDa, testBitbucketRepoUUIDb}, syncedUUIDs(syncer.req))
		result, ok := got.(*bitbucketSyncResult)
		require.True(t, ok)
		assert.Contains(t, result.RenderTable(), "Synced 2 of 4 repositories through account safedep, 2 not enabled")
	})

	t.Run("a workspace with no enabled repository fails with the allowlist command", func(t *testing.T) {
		syncer := &fakeBitbucketProjectSyncer{}
		_, err := runSync(context.Background(), syncDeps{
			bitbucketLinks: &fakeBitbucketLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
				bitbucketLinksPage(bitbucketWorkspaceLink("bb-1")),
			}},
			bitbucketRepositories: &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
				bitbucketRepositoriesPage(listed(testBitbucketRepoUUIDa, disabled)),
			}},
			bitbucketSyncer: syncer,
		}, syncInput{Source: "bitbucket", Account: "safedep", All: true})
		require.Error(t, err)
		assert.Contains(t, errorText(err), "safedep integration bitbucket allowlist update --account safedep")
		assert.Zero(t, syncer.calls)
	})

	t.Run("an account on both sources needs --source", func(t *testing.T) {
		deps, githubSyncer, bitbucketSyncer := newTwoSourceDeps(
			[]*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes{link("gh-1", "safedep")},
			[]*controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink{bitbucketWorkspaceLink("bb-1")},
			nil)

		_, err := runSync(context.Background(), deps, syncInput{Account: "safedep", All: true})
		require.Error(t, err)
		assert.Contains(t, errorText(err), "--source github or --source bitbucket")
		assert.Zero(t, githubSyncer.calls+bitbucketSyncer.calls)
	})

	t.Run("--source settles an account on both sources", func(t *testing.T) {
		deps, githubSyncer, bitbucketSyncer := newTwoSourceDeps(
			[]*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes{link("gh-1", "safedep")},
			[]*controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink{bitbucketWorkspaceLink("bb-1")},
			nil)
		deps.bitbucketRepositories = &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(listed(testBitbucketRepoUUIDa, enabled)),
		}}

		_, err := runSync(context.Background(), deps, syncInput{Source: "bitbucket", Account: "safedep", All: true})
		require.NoError(t, err)
		assert.Zero(t, githubSyncer.calls)
		require.Equal(t, 1, bitbucketSyncer.calls)
		assert.Equal(t, "bb-1", bitbucketSyncer.req.GetLinkId())
	})
}
