package cloudquery

import (
	"context"
	"fmt"
	"math"
	"strings"

	controltowerv2grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v2/controltowerv2grpc"
	controltowerv2 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v2"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

// Service is the production implementation backed by a control-plane gRPC
// connection. Command code accepts the small interfaces in this file
// (ExecRunner, SchemaFetcher) so unit tests can pass stubs.
type Service struct {
	client controltowerv2grpc.QueryServiceClient
}

// NewService wraps a control-plane gRPC connection in a Service.
func NewService(conn *grpc.ClientConn) *Service {
	return &Service{client: controltowerv2grpc.NewQueryServiceClient(conn)}
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
	req := &controltowerv2.QueryBySqlRequest{}
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
	res, err := s.client.GetSchema(ctx, &controltowerv2.GetSchemaRequest{})
	if err != nil {
		return nil, fmt.Errorf("cloudquery: schema: %w", err)
	}

	return decodeSchema(res), nil
}

func decodeResponse(res *controltowerv2.QueryBySqlResponse) *ExecResult {
	out := &ExecResult{
		NextPage: res.GetNextPageToken(),
	}
	if ts := res.GetGeneratedAt(); ts != nil {
		out.GeneratedAt = ts.AsTime()
	}
	if stats := res.GetStats(); stats != nil {
		out.Stats = Stats{
			EstimatedCost: stats.GetEstimatedCost(),
			EstimatedRows: stats.GetEstimatedRows(),
			ElapsedMs:     stats.GetElapsedMs(),
		}
	}

	cols := res.GetColumns()
	out.Columns = make([]Column, 0, len(cols))
	for _, c := range cols {
		out.Columns = append(out.Columns, Column{
			Name: c.GetName(),
			Type: columnTypeName(c.GetType()),
		})
	}

	rows := res.GetRows()
	out.Rows = make([]Row, 0, len(rows))
	for _, raw := range rows {
		decoded := make(Row, len(raw.GetFields()))
		for key, val := range raw.GetFields() {
			decoded[key] = decodeValue(val)
		}
		out.Rows = append(out.Rows, decoded)
	}

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

func decodeSchema(res *controltowerv2.GetSchemaResponse) *Schema {
	src := res.GetTables()
	out := &Schema{Tables: make([]SchemaTable, 0, len(src))}

	for _, t := range src {
		cols := t.GetColumns()
		decoded := SchemaTable{
			Name:              t.GetName(),
			Description:       t.GetDescription(),
			TimeColumn:        t.GetTimeColumn(),
			TimeWindowMaxDays: t.GetTimeWindowMaxDays(),
			Columns:           make([]SchemaColumn, 0, len(cols)),
		}
		for _, c := range cols {
			enums := c.GetEnumValues()
			var decodedEnums []EnumValue
			if len(enums) > 0 {
				decodedEnums = make([]EnumValue, 0, len(enums))
				for _, e := range enums {
					decodedEnums = append(decodedEnums, EnumValue{
						Name:   e.GetName(),
						Number: e.GetNumber(),
					})
				}
			}
			decoded.Columns = append(decoded.Columns, SchemaColumn{
				Name:         c.GetName(),
				Type:         columnTypeName(c.GetType()),
				Description:  c.GetDescription(),
				Selectable:   c.GetSelectable(),
				Filterable:   c.GetFilterable(),
				Groupable:    c.GetGroupable(),
				Aggregatable: c.GetAggregatable(),
				Indexed:      c.GetIndexed(),
				ReferenceURL: c.GetReferenceUrl(),
				EnumValues:   decodedEnums,
			})
		}
		out.Tables = append(out.Tables, decoded)
	}

	edges := res.GetEdges()
	if len(edges) > 0 {
		out.Edges = make([]JoinEdge, 0, len(edges))
		for _, e := range edges {
			out.Edges = append(out.Edges, JoinEdge{
				From:        e.GetFrom(),
				To:          e.GetTo(),
				Cardinality: e.GetCardinality(),
			})
		}
	}

	if usage := res.GetUsage(); usage != nil {
		out.Usage = Usage{
			Rules:          usage.GetRules(),
			ExampleQueries: usage.GetExampleQueries(),
		}
	}

	return out
}

// columnTypeName maps the proto ColumnType enum to the JSON string by
// stripping the COLUMN_TYPE_ prefix. Unknown values (proto returns the
// numeric fallback for those) render as UNSPECIFIED so the proto type
// never leaks to renderers.
func columnTypeName(t controltowerv2.ColumnType) string {
	raw := t.String()
	name, ok := strings.CutPrefix(raw, "COLUMN_TYPE_")
	if !ok || name == "" {
		return "UNSPECIFIED"
	}
	return name
}
