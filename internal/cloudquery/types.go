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
type ExecInput struct {
	SQL      string
	PageSize int
}

// ExecResult is the materialised query response in a presentation-friendly
// shape. Columns is the ordered union of field names across all rows.
type ExecResult struct {
	Columns     []string
	Rows        []Row
	NextPage    string
	GeneratedAt time.Time
}

// Row is one decoded row. Values are typed natives (string, float64,
// bool, nil). Unknown structpb kinds are rendered as their string form so
// callers do not have to handle proto types directly.
type Row map[string]any

// Schema is the full SQL schema served by the control plane.
type Schema struct {
	Tables []SchemaTable
}

// SchemaTable describes one queryable table.
type SchemaTable struct {
	Name    string
	Columns []SchemaColumn
}

// SchemaColumn describes one column inside a table.
type SchemaColumn struct {
	Name         string
	Description  string
	Selectable   bool
	Filterable   bool
	Required     bool
	ReferenceURL string
}
