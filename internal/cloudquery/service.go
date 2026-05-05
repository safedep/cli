package cloudquery

import (
	"context"
	"fmt"
	"math"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

// Service is the production implementation backed by a control-plane gRPC
// connection. Command code accepts the small interfaces in this file
// (ExecRunner, SchemaFetcher) so unit tests can pass stubs.
type Service struct {
	client controltowerv1grpc.QueryServiceClient
}

// NewService wraps a control-plane gRPC connection in a Service.
func NewService(conn *grpc.ClientConn) *Service {
	return &Service{client: controltowerv1grpc.NewQueryServiceClient(conn)}
}

// ExecRunner is the contract a query executor satisfies.
type ExecRunner interface {
	Exec(ctx context.Context, in ExecInput) (*ExecResult, error)
}

// SchemaFetcher is the contract a schema reader satisfies.
type SchemaFetcher interface {
	Schema(ctx context.Context) (*Schema, error)
}

// Exec runs a single SQL statement against the control plane and returns
// the decoded rows. PageSize <= 0 leaves it unset so the server applies
// its default. PageToken empty means "first page".
func (s *Service) Exec(ctx context.Context, in ExecInput) (*ExecResult, error) {
	req := &controltowerv1.QueryBySqlRequest{}
	req.SetQuery(in.SQL)
	if in.PageSize > 0 {
		size := in.PageSize
		if size > math.MaxInt32 {
			size = math.MaxInt32
		}
		req.SetPageSize(int32(size))
	}
	if in.PageToken != "" {
		req.SetPageToken(in.PageToken)
	}

	res, err := s.client.QueryBySql(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("cloudquery: query: %w", err)
	}

	return decodeResponse(res), nil
}

// Schema fetches the SQL schema metadata served by the control plane.
func (s *Service) Schema(ctx context.Context) (*Schema, error) {
	res, err := s.client.GetSqlSchema(ctx, &controltowerv1.GetSqlSchemaRequest{})
	if err != nil {
		return nil, fmt.Errorf("cloudquery: schema: %w", err)
	}

	return decodeSchema(res), nil
}

func decodeResponse(res *controltowerv1.QueryBySqlResponse) *ExecResult {
	out := &ExecResult{
		NextPage: res.GetNextPageToken(),
	}
	if ts := res.GetGeneratedAt(); ts != nil {
		out.GeneratedAt = ts.AsTime()
	}

	rows := res.GetRows()
	out.Rows = make([]Row, 0, len(rows))

	// columnSet preserves first-seen ordering across rows so output is
	// stable when the server returns rows with sparse fields.
	colIndex := map[string]int{}
	var columns []string

	for _, raw := range rows {
		decoded := make(Row, len(raw.GetFields()))
		for key, val := range raw.GetFields() {
			decoded[key] = decodeValue(val)
			if _, ok := colIndex[key]; !ok {
				colIndex[key] = len(columns)
				columns = append(columns, key)
			}
		}
		out.Rows = append(out.Rows, decoded)
	}

	out.Columns = columns
	return out
}

func decodeValue(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	switch v.GetKind().(type) {
	case *structpb.Value_NullValue:
		return nil
	case *structpb.Value_StringValue:
		return v.GetStringValue()
	case *structpb.Value_NumberValue:
		return v.GetNumberValue()
	case *structpb.Value_BoolValue:
		return v.GetBoolValue()
	case *structpb.Value_StructValue:
		return v.GetStructValue().AsMap()
	case *structpb.Value_ListValue:
		raw := v.GetListValue().GetValues()
		out := make([]any, 0, len(raw))
		for _, item := range raw {
			out = append(out, decodeValue(item))
		}
		return out
	default:
		return v.String()
	}
}

func decodeSchema(res *controltowerv1.GetSqlSchemaResponse) *Schema {
	src := res.GetSchemas()
	out := &Schema{Tables: make([]SchemaTable, 0, len(src))}

	for _, t := range src {
		cols := t.GetColumns()
		decoded := SchemaTable{
			Name:    t.GetName(),
			Columns: make([]SchemaColumn, 0, len(cols)),
		}
		for _, c := range cols {
			decoded.Columns = append(decoded.Columns, SchemaColumn{
				Name:         c.GetName(),
				Description:  c.GetDescription(),
				Selectable:   c.GetSelectable(),
				Filterable:   c.GetFilterable(),
				Required:     c.GetRequired(),
				ReferenceURL: c.GetReferenceUrl(),
			})
		}
		out.Tables = append(out.Tables, decoded)
	}

	return out
}
