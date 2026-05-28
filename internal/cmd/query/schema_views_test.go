package query

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/safedep/cli/internal/cloudquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunSchemaList_Counts(t *testing.T) {
	t.Parallel()

	stub := &stubSchemaFetcher{res: &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{
			{Name: "packages", Columns: []cloudquery.SchemaColumn{
				{Name: "name"}, {Name: "version"}, {Name: "id"},
			}},
			{Name: "boms", Columns: []cloudquery.SchemaColumn{{Name: "id"}}},
		},
	}}

	got, err := runSchemaList(context.Background(), stub)
	require.NoError(t, err)
	require.Len(t, got.data.Tables, 2)
	assert.Equal(t, "boms", got.data.Tables[0].Name, "tables must be sorted")
	assert.Equal(t, "packages", got.data.Tables[1].Name)
}

func TestSchemaListResult_RenderTable(t *testing.T) {
	t.Parallel()

	r := &schemaListResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "packages", Description: "OSS packages", Columns: []cloudquery.SchemaColumn{
			{Name: "name"}, {Name: "version"},
		}},
	}}}

	out := r.RenderTable()
	assert.Contains(t, out, "1 tables")
	assert.Contains(t, out, "packages")
	assert.Contains(t, out, "OSS packages")
	assert.Contains(t, out, "2")
	assert.Contains(t, out, "schema show", "table mode must point users at schema show")
}

func TestSchemaListResult_RenderPlain(t *testing.T) {
	t.Parallel()

	r := &schemaListResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "boms", Description: "BOMs", Columns: []cloudquery.SchemaColumn{{Name: "id"}}},
	}}}
	plain := r.RenderPlain()
	assert.Equal(t, "boms\t1\tBOMs", plain)
}

func TestSchemaListResult_RenderJSON(t *testing.T) {
	t.Parallel()

	r := &schemaListResult{data: &cloudquery.Schema{Tables: []cloudquery.SchemaTable{
		{Name: "boms", Description: "BOMs", Columns: []cloudquery.SchemaColumn{{Name: "id"}}},
	}}}
	got, err := r.RenderJSON()
	require.NoError(t, err)

	var parsed schemaListJSON
	require.NoError(t, json.Unmarshal(got, &parsed))
	require.Len(t, parsed.Tables, 1)
	assert.Equal(t, "boms", parsed.Tables[0].Name)
	assert.Equal(t, 1, parsed.Tables[0].Columns)
	assert.Equal(t, "BOMs", parsed.Tables[0].Description)
}

func TestSchemaListResult_RenderEmpty(t *testing.T) {
	t.Parallel()

	r := &schemaListResult{data: &cloudquery.Schema{}}
	assert.Equal(t, "no tables", r.RenderTable())
	assert.Equal(t, "no tables", r.RenderPlain())
}

func TestRunSchemaShow_UnknownTable(t *testing.T) {
	t.Parallel()

	stub := &stubSchemaFetcher{res: sampleSchema()}
	_, err := runSchemaShow(context.Background(), stub, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown table "missing"`)
	assert.Contains(t, err.Error(), "available: projects")
}

func TestRunSchemaShow_EdgesEitherDirection(t *testing.T) {
	t.Parallel()

	in := &cloudquery.Schema{
		Tables: []cloudquery.SchemaTable{
			{Name: "endpoints", Columns: []cloudquery.SchemaColumn{{Name: "id", Type: "STRING"}}},
		},
		Edges: []cloudquery.JoinEdge{
			{From: "inventory_events", To: "endpoints", Cardinality: "many_to_one"},
			{From: "endpoints", To: "projects", Cardinality: "many_to_one"},
			{From: "packages", To: "boms", Cardinality: "many_to_one"}, // unrelated
		},
	}
	stub := &stubSchemaFetcher{res: in}
	got, err := runSchemaShow(context.Background(), stub, "endpoints")
	require.NoError(t, err)
	require.Len(t, got.edges, 2, "must include incoming and outgoing edges")
	assert.Equal(t, "inventory_events", got.edges[0].From)
	assert.Equal(t, "endpoints", got.edges[1].From)
}

func TestSchemaShowResult_RenderTable(t *testing.T) {
	t.Parallel()

	r := &schemaShowResult{
		table: cloudquery.SchemaTable{
			Name: "endpoints", Description: "machines that sync",
			Columns: []cloudquery.SchemaColumn{
				{Name: "id", Type: "STRING", Selectable: true, Indexed: true},
			},
		},
		edges: []cloudquery.JoinEdge{
			{From: "inventory_events", To: "endpoints", Cardinality: "many_to_one"},
		},
	}
	out := r.RenderTable()
	assert.Contains(t, out, "endpoints")
	assert.Contains(t, out, "machines that sync")
	assert.Contains(t, out, "Joins")
	assert.Contains(t, out, "inventory_events -> endpoints (many_to_one)")
}

func TestSchemaShowResult_RenderJSON(t *testing.T) {
	t.Parallel()

	r := &schemaShowResult{
		table: cloudquery.SchemaTable{
			Name: "endpoints",
			Columns: []cloudquery.SchemaColumn{
				{Name: "id", Type: "STRING", Selectable: true},
			},
		},
		edges: []cloudquery.JoinEdge{
			{From: "endpoints", To: "projects", Cardinality: "many_to_one"},
		},
	}
	got, err := r.RenderJSON()
	require.NoError(t, err)

	var parsed schemaShowJSON
	require.NoError(t, json.Unmarshal(got, &parsed))
	assert.Equal(t, "endpoints", parsed.Table.Name)
	require.Len(t, parsed.Table.Columns, 1)
	assert.Equal(t, "STRING", parsed.Table.Columns[0].Type)
	require.Len(t, parsed.Edges, 1)
	assert.Equal(t, "projects", parsed.Edges[0].To)
}

func TestSchemaShowResult_RenderPlain(t *testing.T) {
	t.Parallel()

	r := &schemaShowResult{
		table: cloudquery.SchemaTable{
			Name: "endpoints",
			Columns: []cloudquery.SchemaColumn{
				{Name: "id", Type: "STRING", Selectable: true, Indexed: true},
			},
		},
		edges: []cloudquery.JoinEdge{
			{From: "inventory_events", To: "endpoints", Cardinality: "many_to_one"},
		},
	}
	plain := r.RenderPlain()
	lines := strings.Split(plain, "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	assert.Equal(t, "endpoints.id\tSTRING\tselectable,indexed\t", lines[0])
	assert.Equal(t, "# join: inventory_events -> endpoints (many_to_one)", lines[1])
}
