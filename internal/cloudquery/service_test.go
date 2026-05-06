package cloudquery

import (
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDecodeValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *structpb.Value
		want any
	}{
		{"nil-value", nil, nil},
		{"null", structpb.NewNullValue(), nil},
		{"string", structpb.NewStringValue("foo"), "foo"},
		{"number", structpb.NewNumberValue(3.5), 3.5},
		{"bool-true", structpb.NewBoolValue(true), true},
		{"bool-false", structpb.NewBoolValue(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, decodeValue(tt.in))
		})
	}
}

func TestDecodeValue_List(t *testing.T) {
	t.Parallel()

	list, err := structpb.NewList([]any{"a", float64(1), true})
	require.NoError(t, err)
	got := decodeValue(structpb.NewListValue(list))
	assert.Equal(t, []any{"a", float64(1), true}, got)
}

func TestDecodeResponse_PreservesColumnOrder(t *testing.T) {
	t.Parallel()

	row1, err := structpb.NewStruct(map[string]any{"name": "alpha", "score": float64(1)})
	require.NoError(t, err)
	row2, err := structpb.NewStruct(map[string]any{"score": float64(2), "extra": "yes"})
	require.NoError(t, err)

	resp := &controltowerv1.QueryBySqlResponse{}
	resp.SetRows([]*structpb.Struct{row1, row2})
	resp.SetGeneratedAt(timestamppb.Now())
	resp.SetNextPageToken("tok")

	got := decodeResponse(resp)
	assert.Equal(t, "tok", got.NextPage)
	assert.False(t, got.GeneratedAt.IsZero())
	assert.Len(t, got.Rows, 2)
	// columns from row1 must appear before columns first seen in row2.
	assert.Contains(t, got.Columns, "name")
	assert.Contains(t, got.Columns, "score")
	assert.Contains(t, got.Columns, "extra")
	assertIndexBefore(t, got.Columns, "name", "extra")
	assertIndexBefore(t, got.Columns, "score", "extra")
}

func TestDecodeResponse_EmptyRows(t *testing.T) {
	t.Parallel()

	resp := &controltowerv1.QueryBySqlResponse{}
	got := decodeResponse(resp)
	assert.Empty(t, got.Rows)
	assert.Empty(t, got.Columns)
}

func TestDecodeSchema(t *testing.T) {
	t.Parallel()

	col := &controltowerv1.SqlSchemaColumn{}
	col.SetName("name")
	col.SetDescription("the name")
	col.SetSelectable(true)
	col.SetFilterable(true)
	col.SetReferenceUrl("https://docs.example/name")

	tbl := &controltowerv1.SqlSchema{}
	tbl.SetName("packages")
	tbl.SetColumns([]*controltowerv1.SqlSchemaColumn{col})

	resp := &controltowerv1.GetSqlSchemaResponse{}
	resp.SetSchemas([]*controltowerv1.SqlSchema{tbl})

	got := decodeSchema(resp)
	require.Len(t, got.Tables, 1)
	assert.Equal(t, "packages", got.Tables[0].Name)
	require.Len(t, got.Tables[0].Columns, 1)
	c := got.Tables[0].Columns[0]
	assert.Equal(t, "name", c.Name)
	assert.Equal(t, "the name", c.Description)
	assert.True(t, c.Selectable)
	assert.True(t, c.Filterable)
	assert.Equal(t, "https://docs.example/name", c.ReferenceURL)
}

func assertIndexBefore(t *testing.T, cols []string, a, b string) {
	t.Helper()
	ai, bi := -1, -1
	for i, c := range cols {
		if c == a {
			ai = i
		}
		if c == b {
			bi = i
		}
	}
	require.NotEqual(t, -1, ai, "column %q not found", a)
	require.NotEqual(t, -1, bi, "column %q not found", b)
	assert.Less(t, ai, bi, "expected %q to appear before %q in %v", a, b, cols)
}
