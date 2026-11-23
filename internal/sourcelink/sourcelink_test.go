package sourcelink

import (
	"testing"

	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	ghSafedep = Link{Source: GitHub, ID: "gh-1", Account: "SafeDep"}
	ghAcme    = Link{Source: GitHub, ID: "gh-2", Account: "acme"}
	ghNoName  = Link{Source: GitHub, ID: "gh-3"}
	bbSafedep = Link{Source: Bitbucket, ID: "bb-1", Account: "safedep"}
	bbFoo     = Link{Source: Bitbucket, ID: "bb-2", Account: "foo"}
)

func TestPick(t *testing.T) {
	cases := []struct {
		name     string
		links    []Link
		sel      Selection
		want     Link
		wantCode string
		wantHelp []string
	}{
		{name: "the only link", links: []Link{ghAcme}, want: ghAcme},
		{
			name: "an account matches without regard to case", links: []Link{ghSafedep, ghAcme},
			sel: Selection{Account: "safedep"}, want: ghSafedep,
		},
		{
			name: "a source keeps the other source out", links: []Link{ghSafedep, bbFoo},
			sel: Selection{Source: Bitbucket}, want: bbFoo,
		},
		{
			name: "an account on both sources needs a source", links: []Link{ghSafedep, bbSafedep},
			sel: Selection{Account: "safedep"}, wantCode: usefulerror.ErrBadRequest,
			wantHelp: []string{"--source github or --source bitbucket"},
		},
		{
			name: "a source settles an account on both sources", links: []Link{ghSafedep, bbSafedep},
			sel: Selection{Source: Bitbucket, Account: "safedep"}, want: bbSafedep,
		},
		{
			name: "an unknown account lists the linked accounts", links: []Link{ghSafedep, bbFoo, ghNoName},
			sel: Selection{Account: "nope"}, wantCode: usefulerror.ErrNotFound,
			wantHelp: []string{"Linked accounts: --link-id gh-3 (github), SafeDep (github), foo (bitbucket).", `To add "nope"`},
		},
		{
			name: "an unknown Bitbucket account points to link create", links: []Link{bbFoo},
			sel: Selection{Source: Bitbucket, Account: "nope"}, wantCode: usefulerror.ErrNotFound,
			wantHelp: []string{"safedep integration bitbucket link create"},
		},
		{
			name: "an account never matches a link without an account name", links: []Link{ghNoName},
			sel: Selection{Account: "gh-3"}, wantCode: usefulerror.ErrNotFound,
		},
		{
			name:  "two links of one account list their IDs",
			links: []Link{ghSafedep, {Source: GitHub, ID: "gh-9", Account: "safedep"}},
			sel:   Selection{Account: "safedep"}, wantCode: usefulerror.ErrBadRequest,
			wantHelp: []string{"--link-id set to one of: gh-1, gh-9"},
		},
		{
			name: "more than one link lists accounts and the IDs of unnamed links", links: []Link{ghSafedep, ghAcme, ghNoName},
			wantCode: usefulerror.ErrBadRequest,
			wantHelp: []string{"Retry with one of: --account SafeDep, --account acme, --link-id gh-3."},
		},
		{name: "no link", wantCode: usefulerror.ErrNotFound, wantHelp: []string{"GitHub App, or link a Bitbucket workspace"}},
		{
			name: "a link ID picks its link", links: []Link{ghSafedep, bbFoo},
			sel: Selection{LinkID: "bb-2"}, want: bbFoo,
		},
		{
			name: "a link ID with its own account passes", links: []Link{ghSafedep},
			sel: Selection{LinkID: "gh-1", Account: "SAFEDEP"}, want: ghSafedep,
		},
		{
			name: "a link ID with another account conflicts", links: []Link{ghSafedep, ghAcme},
			sel: Selection{LinkID: "gh-1", Account: "acme"}, wantCode: usefulerror.ErrBadRequest,
			wantHelp: []string{"Drop --link-id and retry with --account acme."},
		},
		{
			name: "an unknown link ID", links: []Link{ghSafedep},
			sel: Selection{LinkID: "missing"}, wantCode: usefulerror.ErrNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Pick(tc.links, tc.sel)
			if tc.wantCode == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				return
			}
			require.Error(t, err)
			usefulErr, ok := usefulerror.AsUsefulError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, usefulErr.Code())
			for _, want := range tc.wantHelp {
				assert.Contains(t, usefulErr.Help(), want)
			}
		})
	}
}

func TestGroupByOwner(t *testing.T) {
	cases := []struct {
		name    string
		links   []Link
		sel     Selection
		names   []string
		want    []Group
		wantErr string
	}{
		{
			name:  "each owner picks its link, in first-seen order",
			links: []Link{ghSafedep, ghAcme},
			names: []string{"acme/a", "safedep/b", "ACME/c"},
			want:  []Group{{Link: ghAcme, Names: []string{"acme/a", "ACME/c"}}, {Link: ghSafedep, Names: []string{"safedep/b"}}},
		},
		{
			name:  "the owner decides the source",
			links: []Link{ghAcme, bbFoo},
			names: []string{"foo/a"},
			want:  []Group{{Link: bbFoo, Names: []string{"foo/a"}}},
		},
		{
			name:  "the only link serves an owner without an account name",
			links: []Link{ghNoName},
			names: []string{"safedep/a"},
			want:  []Group{{Link: ghNoName, Names: []string{"safedep/a"}}},
		},
		{
			name:  "the only link serves an owner its stale account name misses",
			links: []Link{{Source: GitHub, ID: "gh-1", Account: "old-name"}},
			names: []string{"new-name/a"},
			want:  []Group{{Link: Link{Source: GitHub, ID: "gh-1", Account: "old-name"}, Names: []string{"new-name/a"}}},
		},
		{
			name:  "the only link of the selected source serves an unknown owner",
			links: []Link{ghAcme, bbFoo},
			sel:   Selection{Source: Bitbucket},
			names: []string{"other/a"},
			want:  []Group{{Link: bbFoo, Names: []string{"other/a"}}},
		},
		{
			name:    "an unknown owner among several links fails",
			links:   []Link{ghSafedep, ghAcme},
			names:   []string{"safedep/a", "nope/b"},
			wantErr: `no GitHub or Bitbucket link of the active tenant has account "nope"`,
		},
		{
			name:  "an account serves every name",
			links: []Link{ghSafedep, ghAcme},
			sel:   Selection{Account: "acme"},
			names: []string{"safedep/a", "acme/b"},
			want:  []Group{{Link: ghAcme, Names: []string{"safedep/a", "acme/b"}}},
		},
		{
			name:  "a link ID serves every name",
			links: []Link{ghSafedep, bbFoo},
			sel:   Selection{LinkID: "bb-2"},
			names: []string{"safedep/a"},
			want:  []Group{{Link: bbFoo, Names: []string{"safedep/a"}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GroupByOwner(tc.links, tc.sel, tc.names)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
