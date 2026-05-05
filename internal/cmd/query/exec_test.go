package query

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safedep/cli/internal/cloudquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubExecRunner struct {
	got cloudquery.ExecInput
	res *cloudquery.ExecResult
	err error
}

func (s *stubExecRunner) Exec(_ context.Context, in cloudquery.ExecInput) (*cloudquery.ExecResult, error) {
	s.got = in
	if s.err != nil {
		return nil, s.err
	}
	return s.res, nil
}

func TestRunExec_PropagatesError(t *testing.T) {
	t.Parallel()

	stub := &stubExecRunner{err: errors.New("boom")}
	_, err := runExec(context.Background(), stub, cloudquery.ExecInput{SQL: "select 1"})
	require.Error(t, err)
	assert.EqualError(t, err, "boom")
}

func TestRunExec_PassesInputThrough(t *testing.T) {
	t.Parallel()

	stub := &stubExecRunner{res: &cloudquery.ExecResult{}}
	_, err := runExec(context.Background(), stub, cloudquery.ExecInput{SQL: "select 1", PageSize: 50})
	require.NoError(t, err)
	assert.Equal(t, "select 1", stub.got.SQL)
	assert.Equal(t, 50, stub.got.PageSize)
}

func TestExecResult_RenderJSON(t *testing.T) {
	t.Parallel()

	r := &execResult{data: &cloudquery.ExecResult{
		Columns: []string{"name", "score"},
		Rows: []cloudquery.Row{
			{"name": "alpha", "score": float64(1)},
			{"name": "beta", "score": float64(2)},
		},
		NextPage: "tok",
	}}

	got, err := r.RenderJSON()
	require.NoError(t, err)

	var parsed execJSON
	require.NoError(t, json.Unmarshal(got, &parsed))
	assert.Equal(t, []string{"name", "score"}, parsed.Columns)
	assert.Equal(t, 2, parsed.Count)
	assert.Equal(t, "tok", parsed.NextPageToken)
	require.Len(t, parsed.Rows, 2)
}

func TestExecResult_RenderPlainEmpty(t *testing.T) {
	t.Parallel()

	r := &execResult{data: &cloudquery.ExecResult{}}
	assert.Equal(t, "no rows", r.RenderPlain())
	assert.Equal(t, "no rows", r.RenderTable())
}

func TestExecResult_RenderPlain(t *testing.T) {
	t.Parallel()

	r := &execResult{data: &cloudquery.ExecResult{
		Columns: []string{"a", "b"},
		Rows: []cloudquery.Row{
			{"a": "x", "b": float64(2)},
		},
	}}
	plain := r.RenderPlain()
	lines := strings.Split(plain, "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "a\tb", lines[0])
	assert.Equal(t, "x\t2", lines[1])
}

func TestValidateSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty", "", "", true},
		{"whitespace-only", "   ", "", true},
		{"trailing-semi", "select 1;", "select 1", false},
		{"trim", "  select 1  ", "select 1", false},
		{"too-long", strings.Repeat("a", maxSQLBytes+1), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateSQL(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalisePageSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      int
		want    int
		wantErr bool
	}{
		{"zero", 0, 0, true},
		{"negative", -1, 0, true},
		{"valid", 100, 100, false},
		{"max", maxPageSize, maxPageSize, false},
		{"too-large", maxPageSize + 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalisePageSize(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveSQL_FlagPrecedence(t *testing.T) {
	t.Parallel()

	got, err := resolveSQL(strings.NewReader("select stdin"), execInput{SQL: "select flag"})
	require.NoError(t, err)
	assert.Equal(t, "select flag", got)
}

func TestResolveSQL_FromStdin(t *testing.T) {
	t.Parallel()

	got, err := resolveSQL(strings.NewReader("select stdin"), execInput{})
	require.NoError(t, err)
	assert.Equal(t, "select stdin", got)
}

func TestResolveSQL_NoneProvided(t *testing.T) {
	t.Parallel()

	// strings.Reader with empty content reads as EOF; validateSQL rejects.
	_, err := resolveSQL(strings.NewReader(""), execInput{})
	require.Error(t, err)
}

func TestResolveSQL_FromFile(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "q.sql")
	require.NoError(t, os.WriteFile(tmp, []byte("select file"), 0o600))

	got, err := resolveSQL(strings.NewReader("ignored"), execInput{SQLFile: tmp})
	require.NoError(t, err)
	assert.Equal(t, "select file", got)
}
