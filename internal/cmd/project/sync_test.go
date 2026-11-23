package project

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/sourcelink"
)

func TestSyncCmd_PositionalArgumentsAreRepositoryNames(t *testing.T) {
	t.Parallel()

	cmd := syncCmd(nil)

	assert.Equal(t, "sync [OWNER/REPOSITORY...]", cmd.Use)
	require.NotNil(t, cmd.Flags().Lookup("repository-id"))
	assert.Contains(t, cmd.UsageString(), "--account")
	assert.NotContains(t, cmd.UsageString(), "--link-id", "the link ID flag is for scripts, not for people")

	require.NoError(t, cmd.Args(cmd, []string{"safedep/cli"}))

	err := cmd.Args(cmd, []string{"safedep/cli", "SafeDep/CLI"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate repository")
}

func TestSyncCmd_RepositoryIDFlagSatisfiesTheSelection(t *testing.T) {
	t.Parallel()

	cmd := syncCmd(nil)

	require.NoError(t, cmd.ParseFlags([]string{"--repository-id", "1296269,10270250"}))
	require.NoError(t, cmd.Args(cmd, cmd.Flags().Args()))
}

func TestValidateSyncInput(t *testing.T) {
	t.Parallel()

	oneHundred := make([]int64, maxSyncRepositories)
	for i := range oneHundred {
		oneHundred[i] = int64(i + 1)
	}
	oneHundredOne := append(append([]int64{}, oneHundred...), 1_000_000)

	tests := []struct {
		name    string
		in      syncInput
		wantErr string
	}{
		{name: "one name", in: syncInput{RepositoryNames: []string{"safedep/cli"}}},
		{name: "one ID", in: syncInput{RepositoryIDs: []int64{1296269}}},
		{name: "one hundred IDs", in: syncInput{RepositoryIDs: oneHundred}},
		{
			name: "mixed selectors",
			in:   syncInput{RepositoryNames: []string{"safedep/cli"}, RepositoryIDs: []int64{1296269}},
		},
		{name: "nothing selected", wantErr: "between 1 and 100"},
		{name: "over the cap", in: syncInput{RepositoryIDs: oneHundredOne}, wantErr: "between 1 and 100"},
		{
			name:    "missing owner",
			in:      syncInput{RepositoryNames: []string{"cli"}},
			wantErr: "must use the OWNER/REPOSITORY form",
		},
		{
			name:    "empty owner",
			in:      syncInput{RepositoryNames: []string{"/cli"}},
			wantErr: "must use the OWNER/REPOSITORY form",
		},
		{
			name:    "empty repository",
			in:      syncInput{RepositoryNames: []string{"safedep/"}},
			wantErr: "must use the OWNER/REPOSITORY form",
		},
		{
			name:    "nested path",
			in:      syncInput{RepositoryNames: []string{"safedep/group/cli"}},
			wantErr: "must use the OWNER/REPOSITORY form",
		},
		{
			name:    "duplicate name ignoring case",
			in:      syncInput{RepositoryNames: []string{"safedep/cli", "SAFEDEP/CLI"}},
			wantErr: `duplicate repository "SAFEDEP/CLI"`,
		},
		{
			name:    "zero ID",
			in:      syncInput{RepositoryIDs: []int64{0}},
			wantErr: "must be greater than zero",
		},
		{
			name:    "negative ID",
			in:      syncInput{RepositoryIDs: []int64{-1}},
			wantErr: "must be greater than zero",
		},
		{
			name:    "duplicate ID",
			in:      syncInput{RepositoryIDs: []int64{7, 7}},
			wantErr: "duplicate repository ID 7",
		},
		{
			name:    "an account with a slash",
			in:      syncInput{Account: "safedep/cli", RepositoryIDs: []int64{7}},
			wantErr: "is not an account name",
		},
		{name: "--all with --account", in: syncInput{All: true, Account: "safedep"}},
		{name: "--all with a hidden link ID", in: syncInput{All: true, LinkID: "link-1", Source: "github"}},
		{name: "--all without an account", in: syncInput{All: true}, wantErr: "--all needs --account"},
		{
			name:    "--all with a name",
			in:      syncInput{All: true, Account: "safedep", RepositoryNames: []string{"safedep/cli"}},
			wantErr: "--all takes no repository names",
		},
		{
			name:    "--all with a repository ID",
			in:      syncInput{All: true, Account: "safedep", RepositoryIDs: []int64{7}},
			wantErr: "--all takes no repository names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSyncInput(tt.in)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSyncResult_RenderJSON(t *testing.T) {
	t.Parallel()

	safedep := sourcelink.Link{Source: sourcelink.GitHub, ID: "link-1", Account: "safedep"}
	acme := sourcelink.Link{Source: sourcelink.GitHub, ID: "link-2", Account: "acme"}
	projects := []syncedProject{
		{RepositoryID: 1296269, RepositoryName: "safedep/cli", ProjectID: "project-1", Account: "safedep", LinkID: "link-1"},
		{RepositoryID: 10270250, ProjectID: "project-2", Account: "acme", LinkID: "link-2"},
	}

	t.Run("one link keeps the top-level link ID and adds its account", func(t *testing.T) {
		got, err := (&syncResult{links: []sourcelink.Link{safedep}, projects: projects[:1]}).RenderJSON()
		require.NoError(t, err)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal(got, &parsed))
		assert.Equal(t, "link-1", parsed["link_id"])
		assert.Equal(t, "safedep", parsed["account"])
		project := parsed["projects"].([]any)[0].(map[string]any)
		assert.InDelta(t, 1296269, project["repository_id"], 0)
		assert.Equal(t, "safedep/cli", project["repository_name"])
		assert.Equal(t, "project-1", project["project_id"])
		assert.Equal(t, "safedep", project["account"])
		assert.Equal(t, "link-1", project["link_id"])
	})

	t.Run("several links name the link on each project only", func(t *testing.T) {
		got, err := (&syncResult{links: []sourcelink.Link{safedep, acme}, projects: projects}).RenderJSON()
		require.NoError(t, err)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal(got, &parsed))
		assert.NotContains(t, parsed, "link_id")
		assert.NotContains(t, parsed, "account")
		second := parsed["projects"].([]any)[1].(map[string]any)
		assert.NotContains(t, second, "repository_name")
		assert.Equal(t, "acme", second["account"])
	})
}

func TestSyncResult_RenderPlain(t *testing.T) {
	t.Parallel()

	result := &syncResult{
		links: []sourcelink.Link{{Source: sourcelink.GitHub, ID: "link-1"}},
		projects: []syncedProject{
			{RepositoryID: 1296269, RepositoryName: "safedep/cli", ProjectID: "project-1", Account: "safedep", LinkID: "link-1"},
			{RepositoryID: 10270250, ProjectID: "project-2", LinkID: "link-1"},
		},
	}

	lines := strings.Split(result.RenderPlain(), "\n")
	require.Len(t, lines, 3, "plain output is a header plus one line per project, nothing else")
	assert.Equal(t, "repository_name\trepository_id\tproject_id\taccount", lines[0])
	assert.Equal(t, "safedep/cli\t1296269\tproject-1\tsafedep", lines[1])
	assert.Equal(t, "-\t10270250\tproject-2\t-", lines[2])
}

func TestSyncResult_RenderTable(t *testing.T) {
	t.Parallel()

	t.Run("one account", func(t *testing.T) {
		result := &syncResult{
			links: []sourcelink.Link{{Source: sourcelink.GitHub, ID: "link-1", Account: "safedep"}},
			projects: []syncedProject{
				{RepositoryID: 1296269, RepositoryName: "safedep/cli", ProjectID: "project-1", Account: "safedep", LinkID: "link-1"},
			},
		}
		got := result.RenderTable()
		for _, want := range []string{
			"REPOSITORY", "REPOSITORY ID", "PROJECT ID", "ACCOUNT",
			"safedep/cli", "1296269", "project-1",
			"1 repository synced through account safedep",
		} {
			assert.Contains(t, got, want)
		}
		assert.NotContains(t, got, "link-1")
	})

	t.Run("a link without an account name shows its ID", func(t *testing.T) {
		result := &syncResult{
			links:    []sourcelink.Link{{Source: sourcelink.GitHub, ID: "link-1"}},
			projects: []syncedProject{{RepositoryID: 7, ProjectID: "project-7", LinkID: "link-1"}},
		}
		assert.Contains(t, result.RenderTable(), "1 repository synced through link link-1")
	})

	t.Run("several accounts", func(t *testing.T) {
		result := &syncResult{links: []sourcelink.Link{{ID: "link-1"}, {ID: "link-2"}}}
		assert.Contains(t, result.RenderTable(), "0 repositories synced through 2 accounts")
	})
}
