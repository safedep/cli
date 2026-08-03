package project

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncCmd_PositionalArgumentsAreRepositoryNames(t *testing.T) {
	t.Parallel()

	cmd := syncCmd(nil)

	assert.Equal(t, "sync [OWNER/REPOSITORY...]", cmd.Use)
	require.NotNil(t, cmd.Flags().Lookup("link-id"))
	require.NotNil(t, cmd.Flags().Lookup("repository-id"))

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

	result := &syncResult{
		linkID: "link-1",
		projects: []syncedProject{
			{RepositoryID: 1296269, RepositoryName: "safedep/cli", ProjectID: "project-1"},
			{RepositoryID: 10270250, ProjectID: "project-2"},
		},
	}

	got, err := result.RenderJSON()
	require.NoError(t, err)

	var parsed struct {
		LinkID   string           `json:"link_id"`
		Projects []map[string]any `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(got, &parsed))
	assert.Equal(t, "link-1", parsed.LinkID)
	require.Len(t, parsed.Projects, 2)
	assert.InDelta(t, 1296269, parsed.Projects[0]["repository_id"], 0)
	assert.Equal(t, "safedep/cli", parsed.Projects[0]["repository_name"])
	assert.Equal(t, "project-1", parsed.Projects[0]["project_id"])
	assert.NotContains(t, parsed.Projects[1], "repository_name")
}

func TestSyncResult_RenderPlain(t *testing.T) {
	t.Parallel()

	result := &syncResult{
		linkID: "link-1",
		projects: []syncedProject{
			{RepositoryID: 1296269, RepositoryName: "safedep/cli", ProjectID: "project-1"},
			{RepositoryID: 10270250, ProjectID: "project-2"},
		},
	}

	lines := strings.Split(result.RenderPlain(), "\n")
	require.Len(t, lines, 3, "plain output is a header plus one line per project, nothing else")
	assert.Equal(t, "repository_name\trepository_id\tproject_id", lines[0])
	assert.Equal(t, "safedep/cli\t1296269\tproject-1", lines[1])
	assert.Equal(t, "-\t10270250\tproject-2", lines[2])
	for i, line := range lines {
		assert.NotContains(t, line, "link_id", "line %d", i)
	}
}

func TestSyncResult_RenderTable(t *testing.T) {
	t.Parallel()

	result := &syncResult{
		linkID:   "link-1",
		projects: []syncedProject{{RepositoryID: 1296269, RepositoryName: "safedep/cli", ProjectID: "project-1"}},
	}

	got := result.RenderTable()
	for _, want := range []string{
		"REPOSITORY", "REPOSITORY ID", "PROJECT ID",
		"safedep/cli", "1296269", "project-1",
		"1 repository synced through link link-1",
	} {
		assert.Contains(t, got, want)
	}
}
