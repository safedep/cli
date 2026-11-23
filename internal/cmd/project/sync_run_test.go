package project

import (
	"context"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestRunSync_PicksTheLink runs whole syncs against a tenant whose links the
// case sets, and checks which link each sync request went through. Every
// link sees the same repositories, so only link selection decides the
// outcome.
func TestRunSync_PicksTheLink(t *testing.T) {
	type githubLinks = []*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes
	type bitbucketLinks = []*controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink

	cases := []struct {
		name          string
		in            syncInput
		github        githubLinks
		bitbucket     bitbucketLinks
		bitbucketErr  error
		wantGitHub    [][]int64
		wantGitHubVia []string
		wantBitbucket []string
		wantErr       []string
	}{
		{
			name:          "an explicit source and link ID list nothing",
			in:            syncInput{Source: "bitbucket", LinkID: "bb-1", RepositoryUUIDs: []string{testBitbucketRepoUUIDa}},
			wantBitbucket: []string{"bb-1"},
		},
		{
			name:          "a repository ID selects GitHub before any listing",
			in:            syncInput{RepositoryIDs: []int64{7}},
			github:        githubLinks{link("gh-1", "safedep")},
			bitbucket:     bitbucketLinks{bitbucketWorkspaceLinkOf("bb-1", "foo")},
			wantGitHub:    [][]int64{{7}},
			wantGitHubVia: []string{"gh-1"},
		},
		{
			name:          "a repository UUID selects Bitbucket before any listing",
			in:            syncInput{RepositoryUUIDs: []string{testBitbucketRepoUUIDa}},
			github:        githubLinks{link("gh-1", "safedep")},
			bitbucket:     bitbucketLinks{bitbucketWorkspaceLinkOf("bb-1", "foo")},
			wantBitbucket: []string{"bb-1"},
		},
		{
			name:          "a Bitbucket-only tenant syncs a name through its link",
			in:            syncInput{RepositoryNames: []string{"safedep/cli"}},
			bitbucket:     bitbucketLinks{bitbucketWorkspaceLink("bb-1")},
			wantBitbucket: []string{"bb-1"},
		},
		{
			name:          "the owner picks the source when both are linked",
			in:            syncInput{RepositoryNames: []string{"SafeDep/cli"}},
			github:        githubLinks{link("gh-1", "safedep")},
			bitbucket:     bitbucketLinks{bitbucketWorkspaceLinkOf("bb-1", "foo")},
			wantGitHub:    [][]int64{{12}},
			wantGitHubVia: []string{"gh-1"},
		},
		{
			name:          "two owners make one request per link",
			in:            syncInput{RepositoryNames: []string{"safedep/cli", "acme/app"}},
			github:        githubLinks{link("gh-1", "safedep"), link("gh-2", "acme")},
			wantGitHub:    [][]int64{{12}, {13}},
			wantGitHubVia: []string{"gh-1", "gh-2"},
		},
		{
			name:    "an unknown owner lists the linked accounts",
			in:      syncInput{RepositoryNames: []string{"nope/cli"}},
			github:  githubLinks{link("gh-1", "safedep"), link("gh-2", "acme")},
			wantErr: []string{`No linked account named "nope"`, "Linked accounts: acme, safedep."},
		},
		{
			name:          "the only link serves an owner when its login is empty",
			in:            syncInput{RepositoryNames: []string{"safedep/cli"}},
			github:        githubLinks{link("gh-1", "")},
			wantGitHub:    [][]int64{{12}},
			wantGitHubVia: []string{"gh-1"},
		},
		{
			name:          "the only link serves an owner its stale login misses",
			in:            syncInput{RepositoryNames: []string{"safedep/cli"}},
			github:        githubLinks{link("gh-1", "old-name")},
			wantGitHub:    [][]int64{{12}},
			wantGitHubVia: []string{"gh-1"},
		},
		{
			name:      "an owner linked on both sources needs --source",
			in:        syncInput{RepositoryNames: []string{"safedep/cli"}},
			github:    githubLinks{link("gh-1", "safedep")},
			bitbucket: bitbucketLinks{bitbucketWorkspaceLink("bb-1")},
			wantErr:   []string{"--source github or --source bitbucket"},
		},
		{
			name:          "--source settles an owner linked on both sources",
			in:            syncInput{Source: "bitbucket", RepositoryNames: []string{"safedep/cli"}},
			github:        githubLinks{link("gh-1", "safedep")},
			bitbucket:     bitbucketLinks{bitbucketWorkspaceLink("bb-1")},
			wantBitbucket: []string{"bb-1"},
		},
		{
			name:      "names on two sources are refused",
			in:        syncInput{RepositoryNames: []string{"safedep/cli", "foo/app"}},
			github:    githubLinks{link("gh-1", "safedep")},
			bitbucket: bitbucketLinks{bitbucketWorkspaceLinkOf("bb-1", "foo")},
			wantErr:   []string{"Sync each source in its own command"},
		},
		{
			name:          "--account picks the link for repository IDs",
			in:            syncInput{Account: "ACME", RepositoryIDs: []int64{7}},
			github:        githubLinks{link("gh-1", "safedep"), link("gh-2", "acme")},
			wantGitHub:    [][]int64{{7}},
			wantGitHubVia: []string{"gh-2"},
		},
		{
			name:          "--account serves every name, whatever its owner",
			in:            syncInput{Account: "acme", RepositoryNames: []string{"safedep/cli"}},
			github:        githubLinks{link("gh-1", "safedep"), link("gh-2", "acme")},
			wantGitHub:    [][]int64{{12}},
			wantGitHubVia: []string{"gh-2"},
		},
		{
			name:    "repository IDs with two links need --account",
			in:      syncInput{RepositoryIDs: []int64{7}},
			github:  githubLinks{link("gh-1", "safedep"), link("gh-2", "acme")},
			wantErr: []string{"Retry with one of: --account acme, --account safedep."},
		},
		{
			name:    "--link-id and another --account conflict",
			in:      syncInput{LinkID: "gh-1", Account: "acme", RepositoryIDs: []int64{7}},
			github:  githubLinks{link("gh-1", "safedep"), link("gh-2", "acme")},
			wantErr: []string{"Drop --link-id and retry with --account acme."},
		},
		{
			name:          "a hidden link ID picks its source for names when both are linked",
			in:            syncInput{LinkID: "bb-1", RepositoryNames: []string{"safedep/cli"}},
			github:        githubLinks{link("gh-1", "safedep")},
			bitbucket:     bitbucketLinks{bitbucketWorkspaceLink("bb-1")},
			wantBitbucket: []string{"bb-1"},
		},
		{
			name:      "an unknown link ID is not found",
			in:        syncInput{LinkID: "missing", RepositoryNames: []string{"safedep/cli"}},
			github:    githubLinks{link("gh-1", "safedep")},
			bitbucket: bitbucketLinks{bitbucketWorkspaceLink("bb-1")},
			wantErr:   []string{"Source link not found"},
		},
		{
			name:          "a control plane without the Bitbucket RPCs keeps GitHub working",
			in:            syncInput{RepositoryNames: []string{"safedep/cli"}},
			github:        githubLinks{link("gh-1", "safedep")},
			bitbucketErr:  status.Error(codes.Unimplemented, "unknown method"),
			wantGitHub:    [][]int64{{12}},
			wantGitHubVia: []string{"gh-1"},
		},
		{
			name:         "a caller without the Bitbucket permission cannot infer a source",
			in:           syncInput{RepositoryNames: []string{"safedep/cli"}},
			github:       githubLinks{link("gh-1", "safedep")},
			bitbucketErr: status.Error(codes.PermissionDenied, "not allowed"),
			wantErr:      []string{"resolve workspace link"},
		},
		{
			name:         "any other Bitbucket listing failure stops the sync",
			in:           syncInput{RepositoryNames: []string{"safedep/cli"}},
			github:       githubLinks{link("gh-1", "safedep")},
			bitbucketErr: status.Error(codes.Unavailable, "try again"),
			wantErr:      []string{"resolve workspace link"},
		},
		{
			name:    "no links at all is a not-found failure",
			in:      syncInput{RepositoryNames: []string{"safedep/cli"}},
			wantErr: []string{"No source integration link"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, githubSyncer, bitbucketSyncer := newTwoSourceDeps(tc.github, tc.bitbucket, tc.bitbucketErr)

			_, err := runSync(context.Background(), deps, tc.in)
			if len(tc.wantErr) > 0 {
				require.Error(t, err)
				text := errorText(err)
				for _, want := range tc.wantErr {
					assert.Contains(t, text, want)
				}
				assert.Zero(t, githubSyncer.calls+bitbucketSyncer.calls, "a failed selection syncs nothing")
				return
			}
			require.NoError(t, err)

			var via []string
			var ids [][]int64
			for _, req := range githubSyncer.reqs {
				via = append(via, req.GetLinkId())
				ids = append(ids, syncedIDs(req))
			}
			assert.Equal(t, tc.wantGitHubVia, via)
			assert.Equal(t, tc.wantGitHub, ids)

			var bitbucketVia []string
			for _, req := range bitbucketSyncer.reqs {
				bitbucketVia = append(bitbucketVia, req.GetLinkId())
			}
			assert.Equal(t, tc.wantBitbucket, bitbucketVia)
		})
	}
}

func TestRunSync_SourceGitHubListsNoBitbucketLinks(t *testing.T) {
	deps, syncer, _ := newTwoSourceDeps(
		[]*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes{link("gh-1", "safedep")},
		nil, nil)
	bitbucketLinks := &fakeBitbucketLinkLister{}
	deps.bitbucketLinks = bitbucketLinks

	_, err := runSync(context.Background(), deps, syncInput{Source: "github", RepositoryNames: []string{"safedep/cli"}})
	require.NoError(t, err)
	assert.Zero(t, bitbucketLinks.calls)
	assert.Equal(t, 1, syncer.calls)
}

func TestRunSync_ResolvesEveryNameBeforeTheFirstSync(t *testing.T) {
	deps, githubSyncer, _ := newTwoSourceDeps(
		[]*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes{
			link("gh-1", "safedep"), link("gh-2", "acme"),
		}, nil, nil)

	_, err := runSync(context.Background(), deps, syncInput{RepositoryNames: []string{"safedep/cli", "acme/missing"}})
	require.ErrorContains(t, err, `repository "acme/missing" is not accessible`)
	assert.Zero(t, githubSyncer.calls, "a bad name in the second group must stop the first group's sync")
}

func TestRunSync_StopsAtAFailedLinkAndReportsTheSyncedCount(t *testing.T) {
	deps, githubSyncer, _ := newTwoSourceDeps(
		[]*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes{
			link("gh-1", "safedep"), link("gh-2", "acme"),
		}, nil, nil)
	githubSyncer.err = status.Error(codes.FailedPrecondition, "installation suspended")
	githubSyncer.failOnCall = 2

	_, err := runSync(context.Background(), deps, syncInput{RepositoryNames: []string{"safedep/cli", "acme/app"}})
	require.Error(t, err)
	usefulErr, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "Sync stopped after 1 of 2 repositories", usefulErr.HumanError())
	assert.Contains(t, usefulErr.Help(), "installation suspended. Projects exist for the first 1 repositories.")
	assert.NotContains(t, usefulErr.Help(), "Rerun", "a rerun does not fix a suspended installation")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// newTwoSourceDeps fakes a tenant with the given links on both sources. Each
// source lists the same repositories for every link: GitHub safedep/cli (12)
// and acme/app (13), Bitbucket safedep/cli and foo/app.
func newTwoSourceDeps(
	github []*controltowerv1.ListGitHubAppInstallationLinksResponse_IntegrationWithAttributes,
	bitbucket []*controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink,
	bitbucketErr error,
) (syncDeps, *fakeGitHubProjectSyncer, *fakeBitbucketProjectSyncer) {
	githubSyncer := &fakeGitHubProjectSyncer{}
	bitbucketSyncer := &fakeBitbucketProjectSyncer{}
	deps := syncDeps{
		githubLinks: &fakeGitHubLinkLister{pages: []*controltowerv1.ListGitHubAppInstallationLinksResponse{
			linksResponse("", github...),
		}},
		githubRepositories: &fakeGitHubRepositoryLister{pages: []*controltowerv1.ListGitHubInstallationRepositoriesResponse{
			repositoriesResponse("", repository(12, "safedep/cli"), repository(13, "acme/app")),
		}},
		githubSyncer: githubSyncer,
		bitbucketLinks: &fakeBitbucketLinkLister{
			pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{bitbucketLinksPage(bitbucket...)},
			err:   bitbucketErr,
		},
		bitbucketRepositories: &fakeBitbucketRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			bitbucketRepositoriesPage(
				bitbucketListedRepository(testBitbucketRepoUUIDa, "safedep/cli"),
				bitbucketListedRepository(testBitbucketRepoUUIDb, "foo/app"),
			),
		}},
		bitbucketSyncer: bitbucketSyncer,
	}
	return deps, githubSyncer, bitbucketSyncer
}

// errorText joins what a person reads of an error: the cause, the summary
// and the help.
func errorText(err error) string {
	text := err.Error()
	if usefulErr, ok := usefulerror.AsUsefulError(err); ok {
		text += "\n" + usefulErr.HumanError() + "\n" + usefulErr.Help()
	}
	return text
}
