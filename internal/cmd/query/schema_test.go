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
	_, err := runSchema(context.Background(), stub, nil)
	require.Error(t, err)
	assert.EqualError(t, err, "nope")
}

func TestRunSchema_FilterUnknownTable(t *testing.T) {
	t.Parallel()

	stub := &stubSchemaFetcher{res: sampleSchema()}
	_, err := runSchema(context.Background(), stub, []string{"nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown table(s): nope")
	assert.Contains(t, err.Error(), "available: projects")
}

func TestRunSchema_FilterNarrows(t *testing.T) {
	t.Parallel()

	in := &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{
			{Name: "projects", Columns: []cloudquery.SchemaColumn{{Name: "id", Type: "STRING"}}},
			{Name: "packages", Columns: []cloudquery.SchemaColumn{{Name: "id", Type: "STRING"}}},
		},
		Edges: []cloudquery.JoinEdge{
			{From: "packages", To: "projects", Cardinality: "many_to_one"},
			{From: "packages", To: "boms", Cardinality: "many_to_one"},
		},
		Usage: cloudquery.Usage{Rules: []string{"r"}},
	}
	stub := &stubSchemaFetcher{res: in}
	got, err := runSchema(context.Background(), stub, []string{"projects", "packages"})
	require.NoError(t, err)
	require.Len(t, got.data.Tables, 2)
	require.Len(t, got.data.Edges, 1, "edges must narrow to the filter set")
	assert.Equal(t, "projects", got.data.Edges[0].To)
	assert.Equal(t, []string{"r"}, got.data.Usage.Rules)
}

func TestSortSchema_OrdersTablesAndColumns(t *testing.T) {
	t.Parallel()

	in := &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{
			{Name: "zeta", Columns: []cloudquery.SchemaColumn{{Name: "b"}, {Name: "a"}}},
			{Name: "alpha", Columns: []cloudquery.SchemaColumn{{Name: "z"}, {Name: "y"}}},
		},
		Edges: []cloudquery.JoinEdge{{From: "x", To: "y", Cardinality: "one_to_one"}},
		Usage: cloudquery.Usage{Rules: []string{"r"}, ExampleQueries: []string{"q"}},
	}
	out := sortSchema(in)
	require.Len(t, out.Tables, 2)
	assert.Equal(t, "alpha", out.Tables[0].Name)
	assert.Equal(t, "zeta", out.Tables[1].Name)
	assert.Equal(t, "y", out.Tables[0].Columns[0].Name)
	assert.Equal(t, "z", out.Tables[0].Columns[1].Name)
	assert.Equal(t, "a", out.Tables[1].Columns[0].Name)
	assert.Equal(t, "b", out.Tables[1].Columns[1].Name)
	assert.Equal(t, in.Edges, out.Edges)
	assert.Equal(t, in.Usage, out.Usage)
}

func sampleSchema() *cloudquery.Schema {
	return &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{{
			Name:        "projects",
			Description: "projects table",
			Columns: []cloudquery.SchemaColumn{
				{
					Name:       "origin_source",
					Type:       "ENUM",
					Selectable: true, Filterable: true, Groupable: true, Indexed: true,
					EnumValues: []cloudquery.EnumValue{{Name: "SOURCE_GITHUB", Number: 1}},
				},
				{
					Name:       "name",
					Type:       "STRING",
					Selectable: true, Filterable: true, Indexed: true,
					ReferenceURL: "https://docs.example/name",
				},
			},
		}},
		Edges: []cloudquery.JoinEdge{{From: "packages", To: "boms", Cardinality: "many_to_one"}},
		Usage: cloudquery.Usage{
			Rules:          []string{"Every query must filter on an indexed column."},
			ExampleQueries: []string{"SELECT projects.id FROM projects"},
		},
	}
}

func TestSchemaResult_RenderJSON(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: sampleSchema()}
	got, err := r.RenderJSON()
	require.NoError(t, err)

	var parsed schemaJSON
	require.NoError(t, json.Unmarshal(got, &parsed))
	require.Len(t, parsed.Tables, 1)
	tbl := parsed.Tables[0]
	assert.Equal(t, "projects", tbl.Name)
	assert.Equal(t, "projects table", tbl.Description)
	require.Len(t, tbl.Columns, 2)

	c0 := tbl.Columns[0]
	assert.Equal(t, "origin_source", c0.Name)
	assert.Equal(t, "ENUM", c0.Type)
	assert.True(t, c0.Selectable)
	assert.True(t, c0.Groupable)
	assert.True(t, c0.Indexed)
	require.Len(t, c0.EnumValues, 1)
	assert.Equal(t, "SOURCE_GITHUB", c0.EnumValues[0].Name)
	assert.Equal(t, int32(1), c0.EnumValues[0].Number)

	require.Len(t, parsed.Edges, 1)
	assert.Equal(t, schemaEdgeJSON{From: "packages", To: "boms", Cardinality: "many_to_one"}, parsed.Edges[0])
	require.NotNil(t, parsed.Usage)
	assert.Equal(t, []string{"Every query must filter on an indexed column."}, parsed.Usage.Rules)
	assert.Equal(t, []string{"SELECT projects.id FROM projects"}, parsed.Usage.ExampleQueries)
}

func TestSchemaResult_RenderJSON_OmitsEmptyOptional(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{{
			Name: "t",
			Columns: []cloudquery.SchemaColumn{
				{Name: "c", Type: "STRING", Selectable: true},
			},
		}},
	}}
	got, err := r.RenderJSON()
	require.NoError(t, err)

	s := string(got)
	assert.NotContains(t, s, "enum_values")
	assert.NotContains(t, s, "reference_url")
	assert.NotContains(t, s, "time_column")
	assert.NotContains(t, s, "time_window_max_days")
	assert.NotContains(t, s, "\"edges\"")
	assert.NotContains(t, s, "\"usage\"")
}

func TestSchemaResult_RenderPlain(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: sampleSchema()}
	plain := r.RenderPlain()
	lines := strings.Split(plain, "\n")

	require.GreaterOrEqual(t, len(lines), 2)
	assert.True(t, strings.HasPrefix(lines[0], "projects.origin_source\tENUM\t"), "got: %q", lines[0])
	assert.Contains(t, lines[0], "SOURCE_GITHUB")

	assert.Contains(t, plain, "# join: packages -> boms (many_to_one)")
	assert.Contains(t, plain, "# rule: Every query must filter on an indexed column.")
	assert.Contains(t, plain, "# example: SELECT projects.id FROM projects")
}

func TestSchemaResult_RenderTable(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: sampleSchema()}
	out := r.RenderTable()

	assert.Contains(t, out, "projects")
	assert.Contains(t, out, "projects table", "table description must be inline with the header")
	assert.Contains(t, out, "origin_source")
	assert.Contains(t, out, "ENUM")
	assert.Contains(t, out, "SOURCE_GITHUB")
	assert.NotContains(t, out, "Reference", "Reference column must be removed from the main table")
	assert.Contains(t, out, "refs:", "reference URLs must surface as a footnote when present")
	assert.Contains(t, out, "https://docs.example/name")
	assert.Contains(t, out, "Joins")
	assert.Contains(t, out, "many_to_one")
	assert.Contains(t, out, "Usage")
	assert.Contains(t, out, "- Every query must filter")
	assert.Contains(t, out, "Examples")
}

func TestSchemaResult_RenderTable_TruncatesLongEnums(t *testing.T) {
	t.Parallel()

	values := []cloudquery.EnumValue{
		{Name: "A", Number: 1}, {Name: "B", Number: 2}, {Name: "C", Number: 3},
		{Name: "D", Number: 4}, {Name: "E", Number: 5},
	}
	r := &schemaResult{data: &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{{
			Name: "t",
			Columns: []cloudquery.SchemaColumn{
				{Name: "kind", Type: "ENUM", EnumValues: values},
			},
		}},
	}}
	out := r.RenderTable()
	assert.Contains(t, out, "A,B,C (+2 more)")
	assert.NotContains(t, out, "A,B,C,D,E")
}

func TestSchemaResult_RenderTable_GroupsByTable(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "alpha", Columns: []cloudquery.SchemaColumn{{Name: "x", Type: "STRING", Selectable: true}}},
		{Name: "beta", Columns: []cloudquery.SchemaColumn{{Name: "y", Type: "INT", Selectable: true}}},
	}}}
	out := r.RenderTable()
	alpha := strings.Index(out, "alpha")
	beta := strings.Index(out, "beta")
	require.NotEqual(t, -1, alpha)
	require.NotEqual(t, -1, beta)
	assert.Less(t, alpha, beta, "tables must render in order with their own sections")
}

func TestSchemaResult_RenderTable_TableTimeAnnotation(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{
			Name: "events", TimeColumn: "created_at", TimeWindowMaxDays: 30,
			Columns: []cloudquery.SchemaColumn{{Name: "id", Type: "STRING", Selectable: true}},
		},
	}}}
	out := r.RenderTable()
	assert.Contains(t, out, "[time: created_at, max 30d]")
}

func TestShortCaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   cloudquery.SchemaColumn
		want string
	}{
		{"none", cloudquery.SchemaColumn{}, "-"},
		{"select-only", cloudquery.SchemaColumn{Selectable: true}, "sel"},
		{
			"all",
			cloudquery.SchemaColumn{Selectable: true, Filterable: true, Groupable: true, Aggregatable: true, Indexed: true},
			"sel,fil,grp,agg,idx",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, shortCaps(tt.in))
		})
	}
}

func TestSchemaResult_RenderEmpty(t *testing.T) {
	t.Parallel()

	r := &schemaResult{data: &cloudquery.Schema{}}
	assert.Equal(t, "no tables", r.RenderPlain())
	assert.Equal(t, "no tables", r.RenderTable())
}

func TestColumnFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   cloudquery.SchemaColumn
		want string
	}{
		{"none", cloudquery.SchemaColumn{}, "-"},
		{"select-only", cloudquery.SchemaColumn{Selectable: true}, "selectable"},
		{
			"all",
			cloudquery.SchemaColumn{Selectable: true, Filterable: true, Groupable: true, Aggregatable: true, Indexed: true},
			"selectable,filterable,groupable,aggregatable,indexed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, columnFlags(tt.in))
		})
	}
}
