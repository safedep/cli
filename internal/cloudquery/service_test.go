package cloudquery

import (
	"context"
	"testing"

	controltowerv2grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v2/controltowerv2grpc"
	controltowerv2 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestColumnTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   controltowerv2.ColumnType
		want string
	}{
		{controltowerv2.ColumnType_COLUMN_TYPE_UNSPECIFIED, "UNSPECIFIED"},
		{controltowerv2.ColumnType_COLUMN_TYPE_STRING, "STRING"},
		{controltowerv2.ColumnType_COLUMN_TYPE_INT, "INT"},
		{controltowerv2.ColumnType_COLUMN_TYPE_FLOAT, "FLOAT"},
		{controltowerv2.ColumnType_COLUMN_TYPE_BOOL, "BOOL"},
		{controltowerv2.ColumnType_COLUMN_TYPE_TIMESTAMP, "TIMESTAMP"},
		{controltowerv2.ColumnType_COLUMN_TYPE_ENUM, "ENUM"},
		{controltowerv2.ColumnType(999), "UNSPECIFIED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, columnTypeName(tt.in))
		})
	}
}

func TestDecodeResponse_TypedOrderedColumns(t *testing.T) {
	t.Parallel()

	cName := &controltowerv2.ColumnMeta{}
	cName.SetName("name")
	cName.SetType(controltowerv2.ColumnType_COLUMN_TYPE_STRING)

	cScore := &controltowerv2.ColumnMeta{}
	cScore.SetName("score")
	cScore.SetType(controltowerv2.ColumnType_COLUMN_TYPE_INT)

	row1, err := structpb.NewStruct(map[string]any{"name": "alpha", "score": float64(1)})
	require.NoError(t, err)
	row2, err := structpb.NewStruct(map[string]any{"name": "beta", "score": float64(2)})
	require.NoError(t, err)

	stats := &controltowerv2.QueryStats{}
	stats.SetEstimatedCost(1234.5)
	stats.SetEstimatedRows(2)
	stats.SetElapsedMs(9)

	resp := &controltowerv2.QueryBySqlResponse{}
	resp.SetColumns([]*controltowerv2.ColumnMeta{cName, cScore})
	resp.SetRows([]*structpb.Struct{row1, row2})
	resp.SetGeneratedAt(timestamppb.Now())
	resp.SetNextPageToken("tok")
	resp.SetStats(stats)

	got := decodeResponse(resp)
	assert.Equal(t, "tok", got.NextPage)
	assert.False(t, got.GeneratedAt.IsZero())
	require.Len(t, got.Columns, 2)
	assert.Equal(t, Column{Name: "name", Type: "STRING"}, got.Columns[0])
	assert.Equal(t, Column{Name: "score", Type: "INT"}, got.Columns[1])
	assert.Equal(t, Stats{EstimatedCost: 1234.5, EstimatedRows: 2, ElapsedMs: 9}, got.Stats)
	require.Len(t, got.Rows, 2)
	assert.Equal(t, "alpha", got.Rows[0]["name"])
	assert.Equal(t, float64(1), got.Rows[0]["score"])
}

func TestDecodeResponse_EmptyRows(t *testing.T) {
	t.Parallel()

	resp := &controltowerv2.QueryBySqlResponse{}
	got := decodeResponse(resp)
	assert.Empty(t, got.Rows)
	assert.Empty(t, got.Columns)
	assert.Equal(t, Stats{}, got.Stats)
}

func TestDecodeSchema(t *testing.T) {
	t.Parallel()

	enum := &controltowerv2.EnumValue{}
	enum.SetName("SOURCE_GITHUB")
	enum.SetNumber(1)

	col := &controltowerv2.SchemaColumn{}
	col.SetName("origin_source")
	col.SetType(controltowerv2.ColumnType_COLUMN_TYPE_ENUM)
	col.SetDescription("project source")
	col.SetSelectable(true)
	col.SetFilterable(true)
	col.SetGroupable(true)
	col.SetIndexed(true)
	col.SetReferenceUrl("https://docs.example/origin_source")
	col.SetEnumValues([]*controltowerv2.EnumValue{enum})

	plain := &controltowerv2.SchemaColumn{}
	plain.SetName("name")
	plain.SetType(controltowerv2.ColumnType_COLUMN_TYPE_STRING)
	plain.SetSelectable(true)
	plain.SetFilterable(true)
	plain.SetIndexed(true)

	tbl := &controltowerv2.SchemaTable{}
	tbl.SetName("projects")
	tbl.SetDescription("projects table")
	tbl.SetTimeColumn("created_at")
	tbl.SetTimeWindowMaxDays(30)
	tbl.SetColumns([]*controltowerv2.SchemaColumn{col, plain})

	edge := &controltowerv2.SchemaJoinEdge{}
	edge.SetFrom("packages")
	edge.SetTo("boms")
	edge.SetCardinality("many_to_one")

	usage := &controltowerv2.SchemaUsage{}
	usage.SetRules([]string{"rule one"})
	usage.SetExampleQueries([]string{"SELECT 1"})

	resp := &controltowerv2.GetSchemaResponse{}
	resp.SetTables([]*controltowerv2.SchemaTable{tbl})
	resp.SetEdges([]*controltowerv2.SchemaJoinEdge{edge})
	resp.SetUsage(usage)

	got := decodeSchema(resp)
	require.Len(t, got.Tables, 1)
	gotTbl := got.Tables[0]
	assert.Equal(t, "projects", gotTbl.Name)
	assert.Equal(t, "projects table", gotTbl.Description)
	assert.Equal(t, "created_at", gotTbl.TimeColumn)
	assert.Equal(t, int64(30), gotTbl.TimeWindowMaxDays)
	require.Len(t, gotTbl.Columns, 2)

	c0 := gotTbl.Columns[0]
	assert.Equal(t, "origin_source", c0.Name)
	assert.Equal(t, "ENUM", c0.Type)
	assert.True(t, c0.Selectable)
	assert.True(t, c0.Filterable)
	assert.True(t, c0.Groupable)
	assert.False(t, c0.Aggregatable)
	assert.True(t, c0.Indexed)
	assert.Equal(t, "https://docs.example/origin_source", c0.ReferenceURL)
	require.Len(t, c0.EnumValues, 1)
	assert.Equal(t, EnumValue{Name: "SOURCE_GITHUB", Number: 1}, c0.EnumValues[0])

	c1 := gotTbl.Columns[1]
	assert.Equal(t, "name", c1.Name)
	assert.Equal(t, "STRING", c1.Type)
	assert.Empty(t, c1.EnumValues)

	require.Len(t, got.Edges, 1)
	assert.Equal(t, JoinEdge{From: "packages", To: "boms", Cardinality: "many_to_one"}, got.Edges[0])
	assert.Equal(t, []string{"rule one"}, got.Usage.Rules)
	assert.Equal(t, []string{"SELECT 1"}, got.Usage.ExampleQueries)
}

// stubQueryClient lets tests inject canned gRPC responses without standing
// up a server. Both methods return whatever was stored on the struct.
type stubQueryClient struct {
	controltowerv2grpc.QueryServiceClient
	queryRes  *controltowerv2.QueryBySqlResponse
	queryErr  error
	schemaRes *controltowerv2.GetSchemaResponse
	schemaErr error
}

func (s *stubQueryClient) QueryBySql(_ context.Context, _ *controltowerv2.QueryBySqlRequest, _ ...grpc.CallOption) (*controltowerv2.QueryBySqlResponse, error) {
	return s.queryRes, s.queryErr
}

func (s *stubQueryClient) GetSchema(_ context.Context, _ *controltowerv2.GetSchemaRequest, _ ...grpc.CallOption) (*controltowerv2.GetSchemaResponse, error) {
	return s.schemaRes, s.schemaErr
}

// Server-side validation errors must reach stderr verbatim so agents can
// self-correct from the message. Verify the gRPC status survives wrapping.
func TestExec_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	wantMsg := "column 'foo' does not exist on table 'projects'"
	svc := &Service{client: &stubQueryClient{
		queryErr: status.Error(codes.InvalidArgument, wantMsg),
	}}

	_, err := svc.Exec(context.Background(), ExecInput{SQL: "SELECT foo FROM projects"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), wantMsg)

	st, ok := status.FromError(err)
	require.True(t, ok, "wrapped error must still carry gRPC status, got %v", err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), wantMsg, "server message must survive the wrap")
}

func TestSchema_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	wantMsg := "schema unavailable"
	svc := &Service{client: &stubQueryClient{
		schemaErr: status.Error(codes.Unavailable, wantMsg),
	}}

	_, err := svc.Schema(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), wantMsg)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
}
