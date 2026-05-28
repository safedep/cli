// Package cloudquery is the orchestration layer for SafeDep Cloud's SQL
// query service. It owns the gRPC client wiring, the proto-to-Go boundary
// translation, and the value types that command code consumes.
//
// Command packages depend on the small interfaces declared here, not on
// the gRPC types or proto messages. Tests substitute fakes for the
// interfaces; production wiring uses Service.
package cloudquery

import "time"

// ExecInput is the user-facing payload for a SQL execution. Validation is
// the caller's responsibility: orchestration trusts what it receives.
// PageToken empty means "first page"; non-empty resumes a prior response.
type ExecInput struct {
	SQL       string
	PageSize  int
	PageToken string
}

// Column is one result column with its server-declared type. Type is the
// ColumnType name with the COLUMN_TYPE_ prefix stripped (e.g. STRING, INT).
type Column struct {
	Name string
	Type string
}

// Stats is the planner-reported execution summary for a query response.
type Stats struct {
	EstimatedCost float64
	EstimatedRows int64
	ElapsedMs     int64
}

// ExecResult is the materialised query response in a presentation-friendly
// shape. Columns is the ordered set of columns the server returned.
type ExecResult struct {
	Columns     []Column
	Rows        []Row
	NextPage    string
	GeneratedAt time.Time
	Stats       Stats
}

// Row is one decoded row. Values are typed natives (string, float64,
// bool, nil). Unknown structpb kinds are rendered as their string form so
// callers do not have to handle proto types directly.
type Row map[string]any

// Schema is the full SQL schema served by the control plane.
type Schema struct {
	Tables []SchemaTable
	Edges  []JoinEdge
	Usage  Usage
}

// SchemaTable describes one queryable table.
type SchemaTable struct {
	Name              string
	Description       string
	Columns           []SchemaColumn
	TimeColumn        string
	TimeWindowMaxDays int64
}

// SchemaColumn describes one column inside a table.
type SchemaColumn struct {
	Name         string
	Type         string
	Description  string
	Selectable   bool
	Filterable   bool
	Groupable    bool
	Aggregatable bool
	Indexed      bool
	ReferenceURL string
	EnumValues   []EnumValue
}

// EnumValue is one server-advertised value for an enum-typed column.
type EnumValue struct {
	Name   string
	Number int32
}

// JoinEdge is an advertised join relationship between two tables. Cardinality
// is the server string verbatim (one_to_one, one_to_many, many_to_one).
type JoinEdge struct {
	From        string
	To          string
	Cardinality string
}

// Usage is the server's grammar guidance: rules to follow and example queries.
type Usage struct {
	Rules          []string
	ExampleQueries []string
}
