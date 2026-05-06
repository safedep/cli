package query

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/safedep/cli/internal/cloudquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSchemaFetcher struct {
	res *cloudquery.Schema
	err error
}

func (s *stubSchemaFetcher) Schema(_ context.Context) (*cloudquery.Schema, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.res, nil
}

func TestRunSchema_PropagatesError(t *testing.T) {
	t.Parallel()

	stub := &stubSchemaFetcher{err: errors.New("nope")}
	_, err := runSchema(context.Background(), stub)
	require.Error(t, err)
	assert.EqualError(t, err, "nope")
}

func TestSortSchema_OrdersTablesAndColumns(t *testing.T) {
	t.Parallel()

	in := &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "zeta", Columns: []cloudquery.SchemaColumn{{Name: "b"}, {Name: "a"}}},
		{Name: "alpha", Columns: []cloudquery.SchemaColumn{{Name: "z"}, {Name: "y"}}},
	}}
	out := sortSchema(in)
	require.Len(t, out.Tables, 2)
	assert.Equal(t, "alpha", out.Tables[0].Name)
	assert.Equal(t, "zeta", out.Tables[1].Name)
	assert.Equal(t, "y", out.Tables[0].Columns[0].Name)
	assert.Equal(t, "z", out.Tables[0].Columns[1].Name)
	assert.Equal(t, "a", out.Tables[1].Columns[0].Name)
	assert.Equal(t, "b", out.Tables[1].Columns[1].Name)
}

func TestSchemaResult_RenderJSON(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "packages", Columns: []cloudquery.SchemaColumn{
			{Name: "name", Selectable: true, Filterable: true, ReferenceURL: "https://x"},
		}},
	}}}

	got, err := r.RenderJSON()
	require.NoError(t, err)

	var parsed schemaJSON
	require.NoError(t, json.Unmarshal(got, &parsed))
	require.Len(t, parsed.Tables, 1)
	assert.Equal(t, "packages", parsed.Tables[0].Name)
	require.Len(t, parsed.Tables[0].Columns, 1)
	assert.Equal(t, "name", parsed.Tables[0].Columns[0].Name)
	assert.True(t, parsed.Tables[0].Columns[0].Selectable)
	assert.True(t, parsed.Tables[0].Columns[0].Filterable)
}

func TestSchemaResult_RenderPlain(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "packages", Columns: []cloudquery.SchemaColumn{
			{Name: "name", Selectable: true},
		}},
	}}}

	plain := r.RenderPlain()
	assert.True(t, strings.HasPrefix(plain, "packages.name\t"), "unexpected plain output: %q", plain)
}

func TestSchemaResult_RenderEmpty(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{}}
	assert.Equal(t, "no tables", r.RenderPlain())
	assert.Equal(t, "no tables", r.RenderTable())
}
