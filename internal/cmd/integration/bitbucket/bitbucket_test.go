package bitbucket

import (
	"context"
	"errors"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/bitbucketlink"
)

type fakeLinkCodeCreator struct {
	calls int
	res   *controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse
	err   error
}

func (f *fakeLinkCodeCreator) CreateBitbucketWorkspaceLinkCode(
	_ context.Context,
	_ *controltowerv1.CreateBitbucketWorkspaceLinkCodeRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse, error) {
	f.calls++
	return f.res, f.err
}

type fakeLinkLister struct {
	pages []*controltowerv1.ListBitbucketWorkspaceLinksResponse
	calls int
	err   error
}

func (f *fakeLinkLister) ListBitbucketWorkspaceLinks(
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

type fakeRepositoryLister struct {
	pages    []*controltowerv1.ListBitbucketRepositoriesResponse
	requests []*controltowerv1.ListBitbucketRepositoriesRequest
	err      error
}

func (f *fakeRepositoryLister) ListBitbucketRepositories(
	_ context.Context,
	req *controltowerv1.ListBitbucketRepositoriesRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListBitbucketRepositoriesResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.pages) == 0 {
		return nil, errors.New("unexpected ListBitbucketRepositories call")
	}
	return f.pages[min(len(f.requests)-1, len(f.pages)-1)], nil
}

type fakeAllowlistUpdater struct {
	calls int
	req   *controltowerv1.UpdateBitbucketRepositoryAllowlistRequest
	res   *controltowerv1.UpdateBitbucketRepositoryAllowlistResponse
	err   error
}

func (f *fakeAllowlistUpdater) UpdateBitbucketRepositoryAllowlist(
	_ context.Context,
	req *controltowerv1.UpdateBitbucketRepositoryAllowlistRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.UpdateBitbucketRepositoryAllowlistResponse, error) {
	f.calls++
	f.req = req
	return f.res, f.err
}

var (
	_ linkCodeCreator      = (*fakeLinkCodeCreator)(nil)
	_ bitbucketlink.Lister = (*fakeLinkLister)(nil)
	_ repositoryLister     = (*fakeRepositoryLister)(nil)
	_ allowlistUpdater     = (*fakeAllowlistUpdater)(nil)
)

func newWorkspaceLinkPage(nextPageToken string, links ...*controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink) *controltowerv1.ListBitbucketWorkspaceLinksResponse {
	res := &controltowerv1.ListBitbucketWorkspaceLinksResponse{}
	res.SetLinks(links)
	if nextPageToken != "" {
		pagination := &messagescontroltowerv1.PaginationResponse{}
		pagination.SetNextPageToken(nextPageToken)
		res.SetPagination(pagination)
	}
	return res
}

func newWorkspaceLink(linkID, uuid, slug, name string) *controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink {
	link := &controltowerv1.ListBitbucketWorkspaceLinksResponse_WorkspaceLink{}
	link.SetLinkId(linkID)
	link.SetWorkspaceUuid(uuid)
	link.SetWorkspaceSlug(slug)
	link.SetWorkspaceName(name)
	return link
}

func newRepositoryPage(nextPageToken string, scope controltowerv1.BitbucketScanScope, repositories ...*controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository) *controltowerv1.ListBitbucketRepositoriesResponse {
	res := &controltowerv1.ListBitbucketRepositoriesResponse{}
	res.SetRepositories(repositories)
	res.SetScanScope(scope)
	if nextPageToken != "" {
		pagination := &messagescontroltowerv1.PaginationResponse{}
		pagination.SetNextPageToken(nextPageToken)
		res.SetPagination(pagination)
	}
	return res
}

func newRepository(uuid, fullName string, scanState controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_ScanState) *controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository {
	repository := &controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository{}
	repository.SetRepositoryUuid(uuid)
	repository.SetFullName(fullName)
	repository.SetVisibility(controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_VISIBILITY_PRIVATE)
	repository.SetDefaultBranch("main")
	repository.SetScanState(scanState)
	return repository
}

func TestRunLinkCreate(t *testing.T) {
	expiresAt := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	t.Run("returns the code and its expiry", func(t *testing.T) {
		res := &controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse{}
		res.SetLinkCode("code-1234")
		res.SetExpiresAt(timestamppb.New(expiresAt))
		creator := &fakeLinkCodeCreator{res: res}

		result, err := runLinkCreate(context.Background(), creator)
		require.NoError(t, err)
		assert.Equal(t, 1, creator.calls)
		assert.Equal(t, "code-1234", result.linkCode)
		require.NotNil(t, result.expiresAt)
		assert.Equal(t, expiresAt, *result.expiresAt)
	})

	t.Run("rejects a response without a code", func(t *testing.T) {
		creator := &fakeLinkCodeCreator{res: &controltowerv1.CreateBitbucketWorkspaceLinkCodeResponse{}}

		_, err := runLinkCreate(context.Background(), creator)
		require.ErrorContains(t, err, "invalid response: missing link code")
	})

	t.Run("wraps an RPC failure", func(t *testing.T) {
		creator := &fakeLinkCodeCreator{err: errors.New("boom")}

		_, err := runLinkCreate(context.Background(), creator)
		require.ErrorContains(t, err, "integration bitbucket link create: boom")
	})
}

func TestRunLinkList(t *testing.T) {
	t.Run("walks every page", func(t *testing.T) {
		lister := &fakeLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			newWorkspaceLinkPage("page-2", newWorkspaceLink("link-1", "uuid-1", "one", "One")),
			newWorkspaceLinkPage("", newWorkspaceLink("link-2", "uuid-2", "two", "Two")),
		}}

		result, err := runLinkList(context.Background(), lister)
		require.NoError(t, err)
		assert.Equal(t, 2, lister.calls)
		require.Len(t, result.links, 2)
		assert.Equal(t, "link-1", result.links[0].ID)
		assert.Equal(t, "uuid-2", result.links[1].WorkspaceUUID)
	})

	t.Run("rejects a link without an ID", func(t *testing.T) {
		lister := &fakeLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			newWorkspaceLinkPage("", newWorkspaceLink("", "uuid-1", "one", "One")),
		}}

		_, err := runLinkList(context.Background(), lister)
		require.ErrorContains(t, err, "invalid response: link 1 is missing its ID")
	})
}

func TestRunRepositoryList(t *testing.T) {
	t.Run("resolves the only link and translates repositories", func(t *testing.T) {
		links := &fakeLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			newWorkspaceLinkPage("", newWorkspaceLink("link-1", "uuid-1", "one", "One")),
		}}
		repositories := &fakeRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			newRepositoryPage("page-2", controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED,
				newRepository("repo-uuid-1", "one/alpha", controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_ENABLED)),
			newRepositoryPage("", controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED,
				newRepository("repo-uuid-2", "one/beta", controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_DISABLED)),
		}}

		result, err := runRepositoryList(context.Background(), links, repositories, repositoryListInput{})
		require.NoError(t, err)
		assert.Equal(t, "link-1", result.linkID)
		assert.Equal(t, "selected", result.scanScope)
		require.Len(t, result.repositories, 2)
		assert.Equal(t, listedRepository{
			RepositoryUUID: "repo-uuid-1",
			FullName:       "one/alpha",
			Visibility:     "private",
			DefaultBranch:  "main",
			ScanState:      "enabled",
		}, result.repositories[0])
		assert.Equal(t, "disabled", result.repositories[1].ScanState)
		require.Len(t, repositories.requests, 2)
		assert.Equal(t, "link-1", repositories.requests[0].GetLinkId())
	})

	t.Run("uses the supplied link without resolution", func(t *testing.T) {
		links := &fakeLinkLister{}
		repositories := &fakeRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			newRepositoryPage("", controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_ALL),
		}}

		result, err := runRepositoryList(context.Background(), links, repositories, repositoryListInput{LinkID: "link-9"})
		require.NoError(t, err)
		assert.Equal(t, 0, links.calls)
		assert.Equal(t, "link-9", result.linkID)
		assert.Equal(t, "all", result.scanScope)
		assert.Empty(t, result.repositories)
	})

	t.Run("fails without a linked workspace", func(t *testing.T) {
		links := &fakeLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			newWorkspaceLinkPage(""),
		}}

		_, err := runRepositoryList(context.Background(), links, &fakeRepositoryLister{}, repositoryListInput{})
		require.Error(t, err)
		usefulErr, ok := usefulerror.AsUsefulError(err)
		require.True(t, ok)
		assert.Equal(t, usefulerror.ErrNotFound, usefulErr.Code())
	})

	t.Run("fails on an ambiguous link", func(t *testing.T) {
		links := &fakeLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			newWorkspaceLinkPage("",
				newWorkspaceLink("link-1", "uuid-1", "one", "One"),
				newWorkspaceLink("link-2", "uuid-2", "two", "Two")),
		}}

		_, err := runRepositoryList(context.Background(), links, &fakeRepositoryLister{}, repositoryListInput{})
		require.Error(t, err)
		usefulErr, ok := usefulerror.AsUsefulError(err)
		require.True(t, ok)
		assert.Contains(t, usefulErr.Help(), "link-1 (one)")
		assert.Contains(t, usefulErr.Help(), "link-2 (two)")
	})

	t.Run("rejects a repository without a UUID", func(t *testing.T) {
		repositories := &fakeRepositoryLister{pages: []*controltowerv1.ListBitbucketRepositoriesResponse{
			newRepositoryPage("", controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED,
				newRepository("", "one/alpha", controltowerv1.ListBitbucketRepositoriesResponse_BitbucketRepository_SCAN_STATE_ENABLED)),
		}}

		_, err := runRepositoryList(context.Background(), &fakeLinkLister{}, repositories, repositoryListInput{LinkID: "link-1"})
		require.ErrorContains(t, err, "invalid response: repository 1 is missing its UUID")
	})
}

const (
	testRepoUUIDa = "d6c2f02b-7f78-49a8-b7e3-cf8b86efd115"
	testRepoUUIDb = "9a2b3c4d-0000-4000-8000-000000000001"
	testRepoUUIDc = "9a2b3c4d-0000-4000-8000-000000000002"
)

func TestRunAllowlistUpdate(t *testing.T) {
	newUpdaterResponse := func(scope controltowerv1.BitbucketScanScope, count uint32) *controltowerv1.UpdateBitbucketRepositoryAllowlistResponse {
		res := &controltowerv1.UpdateBitbucketRepositoryAllowlistResponse{}
		res.SetScanScope(scope)
		res.SetAllowlistedRepositoryCount(count)
		return res
	}

	t.Run("sends the delta and reports the outcome", func(t *testing.T) {
		updater := &fakeAllowlistUpdater{
			res: newUpdaterResponse(controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED, 2),
		}

		result, err := runAllowlistUpdate(context.Background(), &fakeLinkLister{}, updater, allowlistUpdateInput{
			LinkID:  "link-1",
			Scope:   "selected",
			Enable:  []string{testRepoUUIDa, testRepoUUIDb},
			Disable: []string{testRepoUUIDc},
		})
		require.NoError(t, err)
		require.Equal(t, 1, updater.calls)
		assert.Equal(t, "link-1", updater.req.GetLinkId())
		assert.Equal(t, controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED, updater.req.GetScanScope())
		assert.Equal(t, []string{testRepoUUIDa, testRepoUUIDb}, updater.req.GetEnable())
		assert.Equal(t, []string{testRepoUUIDc}, updater.req.GetDisable())
		assert.Equal(t, "selected", result.scanScope)
		assert.Equal(t, uint32(2), result.allowlistCount)
	})

	t.Run("normalizes braced uppercase UUIDs before the request", func(t *testing.T) {
		updater := &fakeAllowlistUpdater{
			res: newUpdaterResponse(controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED, 1),
		}

		_, err := runAllowlistUpdate(context.Background(), &fakeLinkLister{}, updater, allowlistUpdateInput{
			LinkID: "link-1",
			Enable: []string{"{D6C2F02B-7F78-49A8-B7E3-CF8B86EFD115}"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{testRepoUUIDa}, updater.req.GetEnable())
	})

	t.Run("keeps the stored scope when the flag is omitted", func(t *testing.T) {
		updater := &fakeAllowlistUpdater{
			res: newUpdaterResponse(controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_SELECTED, 1),
		}

		_, err := runAllowlistUpdate(context.Background(), &fakeLinkLister{}, updater, allowlistUpdateInput{
			LinkID: "link-1",
			Enable: []string{testRepoUUIDa},
		})
		require.NoError(t, err)
		assert.Equal(t, controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_UNSPECIFIED, updater.req.GetScanScope())
	})

	t.Run("resolves the only link", func(t *testing.T) {
		links := &fakeLinkLister{pages: []*controltowerv1.ListBitbucketWorkspaceLinksResponse{
			newWorkspaceLinkPage("", newWorkspaceLink("link-1", "uuid-1", "one", "One")),
		}}
		updater := &fakeAllowlistUpdater{
			res: newUpdaterResponse(controltowerv1.BitbucketScanScope_BITBUCKET_SCAN_SCOPE_ALL, 0),
		}

		result, err := runAllowlistUpdate(context.Background(), links, updater, allowlistUpdateInput{Scope: "all"})
		require.NoError(t, err)
		assert.Equal(t, "link-1", updater.req.GetLinkId())
		assert.Equal(t, "all", result.scanScope)
	})

	t.Run("validation failures", func(t *testing.T) {
		cases := []struct {
			name string
			in   allowlistUpdateInput
			want string
		}{
			{
				name: "nothing to update",
				in:   allowlistUpdateInput{LinkID: "link-1"},
				want: "nothing to update",
			},
			{
				name: "unknown scope",
				in:   allowlistUpdateInput{LinkID: "link-1", Scope: "some"},
				want: "unknown --scope value",
			},
			{
				name: "scope all with a delta",
				in:   allowlistUpdateInput{LinkID: "link-1", Scope: "all", Enable: []string{testRepoUUIDa}},
				want: "scope all takes no --enable or --disable values",
			},
			{
				name: "duplicate enable",
				in:   allowlistUpdateInput{LinkID: "link-1", Enable: []string{testRepoUUIDa, testRepoUUIDa}},
				want: "duplicate --enable value",
			},
			{
				name: "empty disable",
				in:   allowlistUpdateInput{LinkID: "link-1", Disable: []string{""}},
				want: `invalid repository UUID ""`,
			},
			{
				name: "malformed enable",
				in:   allowlistUpdateInput{LinkID: "link-1", Enable: []string{"not-a-uuid"}},
				want: `invalid repository UUID "not-a-uuid"`,
			},
			{
				name: "two spellings of one UUID are a duplicate",
				in:   allowlistUpdateInput{LinkID: "link-1", Enable: []string{testRepoUUIDa, "{D6C2F02B-7F78-49A8-B7E3-CF8B86EFD115}"}},
				want: "duplicate --enable value",
			},
			{
				name: "enable and disable overlap",
				in:   allowlistUpdateInput{LinkID: "link-1", Enable: []string{testRepoUUIDa}, Disable: []string{testRepoUUIDa}},
				want: "in both --enable and --disable",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				updater := &fakeAllowlistUpdater{}

				_, err := runAllowlistUpdate(context.Background(), &fakeLinkLister{}, updater, tc.in)
				require.ErrorContains(t, err, tc.want)
				assert.Equal(t, 0, updater.calls)
			})
		}
	})
}
