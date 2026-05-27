package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudquery"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

func schemaCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "schema",
		Short: "Inspect the SafeDep Cloud query schema",
		Long:  "Inspect the queryable tables and columns served by SafeDep Cloud's query service.",
	}

	parent.AddCommand(schemaGetCmd(a))
	return parent
}

func schemaGetCmd(a *app.App) *cobra.Command {
	var tables []string

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the SafeDep Cloud query schema",
		Long: "Fetch the SQL schema served by SafeDep Cloud: tables and columns with types, " +
			"capability flags, enum values, join edges, and the server's usage rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			svc := cloudquery.NewService(client.Connection())
			result, err := runSchema(cmd.Context(), svc, tables)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	cmd.Flags().StringSliceVar(&tables, "table", nil,
		"limit output to the named table (repeatable, comma-separated also accepted)")
	return cmd
}

func runSchema(ctx context.Context, fetcher cloudquery.SchemaFetcher, tableFilter []string) (*schemaResult, error) {
	res, err := fetcher.Schema(ctx)
	if err != nil {
		return nil, err
	}
	sorted := sortSchema(res)
	filtered, err := filterSchemaByTable(sorted, tableFilter)
	if err != nil {
		return nil, err
	}
	return &schemaResult{data: filtered}, nil
}

// filterSchemaByTable narrows the schema to the requested table names. When
// filter is empty the schema passes through. Unknown names produce an error
// that lists the available tables. Edges narrow to those whose endpoints both
// fall inside the filter set; usage carries through.
func filterSchemaByTable(s *cloudquery.Schema, filter []string) (*cloudquery.Schema, error) {
	if len(filter) == 0 {
		return s, nil
	}

	want := make(map[string]bool, len(filter))
	for _, name := range filter {
		want[strings.TrimSpace(name)] = true
	}

	have := make(map[string]bool, len(s.Tables))
	for _, tbl := range s.Tables {
		have[tbl.Name] = true
	}

	var missing []string
	for name := range want {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		names := make([]string, 0, len(s.Tables))
		for _, tbl := range s.Tables {
			names = append(names, tbl.Name)
		}
		return nil, fmt.Errorf("unknown table(s): %s (available: %s)",
			strings.Join(missing, ", "), strings.Join(names, ", "))
	}

	out := &cloudquery.Schema{Usage: s.Usage}
	out.Tables = make([]cloudquery.SchemaTable, 0, len(want))
	for _, tbl := range s.Tables {
		if want[tbl.Name] {
			out.Tables = append(out.Tables, tbl)
		}
	}
	for _, e := range s.Edges {
		if want[e.From] && want[e.To] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out, nil
}

// sortSchema returns a copy with tables and columns sorted by name so
// output is deterministic across runs and presentation modes. Edges and
// usage carry through unchanged.
func sortSchema(s *cloudquery.Schema) *cloudquery.Schema {
	out := &cloudquery.Schema{
		Tables: make([]cloudquery.SchemaTable, len(s.Tables)),
		Edges:  s.Edges,
		Usage:  s.Usage,
	}
	copy(out.Tables, s.Tables)
	sort.Slice(out.Tables, func(i, j int) bool {
		return out.Tables[i].Name < out.Tables[j].Name
	})
	for i := range out.Tables {
		cols := make([]cloudquery.SchemaColumn, len(out.Tables[i].Columns))
		copy(cols, out.Tables[i].Columns)
		sort.Slice(cols, func(a, b int) bool {
			return cols[a].Name < cols[b].Name
		})
		out.Tables[i].Columns = cols
	}
	return out
}

type schemaResult struct {
	data *cloudquery.Schema
}

type schemaJSON struct {
	Tables []schemaTableJSON `json:"tables"`
	Edges  []schemaEdgeJSON  `json:"edges,omitempty"`
	Usage  *schemaUsageJSON  `json:"usage,omitempty"`
}

type schemaTableJSON struct {
	Name              string             `json:"name"`
	Description       string             `json:"description,omitempty"`
	Columns           []schemaColumnJSON `json:"columns"`
	TimeColumn        string             `json:"time_column,omitempty"`
	TimeWindowMaxDays int64              `json:"time_window_max_days,omitempty"`
}

type schemaColumnJSON struct {
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	Description  string           `json:"description,omitempty"`
	Selectable   bool             `json:"selectable"`
	Filterable   bool             `json:"filterable"`
	Groupable    bool             `json:"groupable,omitempty"`
	Aggregatable bool             `json:"aggregatable,omitempty"`
	Indexed      bool             `json:"indexed,omitempty"`
	ReferenceURL string           `json:"reference_url,omitempty"`
	EnumValues   []schemaEnumJSON `json:"enum_values,omitempty"`
}

type schemaEnumJSON struct {
	Name   string `json:"name"`
	Number int32  `json:"number"`
}

type schemaEdgeJSON struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Cardinality string `json:"cardinality,omitempty"`
}

type schemaUsageJSON struct {
	Rules          []string `json:"rules,omitempty"`
	ExampleQueries []string `json:"example_queries,omitempty"`
}

func (r *schemaResult) RenderJSON() ([]byte, error) {
	out := schemaJSON{Tables: make([]schemaTableJSON, 0, len(r.data.Tables))}
	for _, tbl := range r.data.Tables {
		cols := make([]schemaColumnJSON, 0, len(tbl.Columns))
		for _, c := range tbl.Columns {
			cols = append(cols, schemaColumnJSON{
				Name:         c.Name,
				Type:         c.Type,
				Description:  c.Description,
				Selectable:   c.Selectable,
				Filterable:   c.Filterable,
				Groupable:    c.Groupable,
				Aggregatable: c.Aggregatable,
				Indexed:      c.Indexed,
				ReferenceURL: c.ReferenceURL,
				EnumValues:   enumValuesJSON(c.EnumValues),
			})
		}
		out.Tables = append(out.Tables, schemaTableJSON{
			Name:              tbl.Name,
			Description:       tbl.Description,
			Columns:           cols,
			TimeColumn:        tbl.TimeColumn,
			TimeWindowMaxDays: tbl.TimeWindowMaxDays,
		})
	}
	if len(r.data.Edges) > 0 {
		out.Edges = make([]schemaEdgeJSON, 0, len(r.data.Edges))
		for _, e := range r.data.Edges {
			out.Edges = append(out.Edges, schemaEdgeJSON{From: e.From, To: e.To, Cardinality: e.Cardinality})
		}
	}
	if len(r.data.Usage.Rules) > 0 || len(r.data.Usage.ExampleQueries) > 0 {
		out.Usage = &schemaUsageJSON{
			Rules:          r.data.Usage.Rules,
			ExampleQueries: r.data.Usage.ExampleQueries,
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

func enumValuesJSON(vs []cloudquery.EnumValue) []schemaEnumJSON {
	if len(vs) == 0 {
		return nil
	}
	out := make([]schemaEnumJSON, 0, len(vs))
	for _, v := range vs {
		out = append(out, schemaEnumJSON{Name: v.Name, Number: v.Number})
	}
	return out
}

func (r *schemaResult) RenderPlain() string {
	if len(r.data.Tables) == 0 && len(r.data.Edges) == 0 &&
		len(r.data.Usage.Rules) == 0 && len(r.data.Usage.ExampleQueries) == 0 {
		return "no tables"
	}

	var sb strings.Builder
	for _, tbl := range r.data.Tables {
		for _, c := range tbl.Columns {
			fmt.Fprintf(&sb, "%s.%s\t%s\t%s\t%s\n",
				tbl.Name, c.Name, c.Type, columnFlags(c), enumNamesCSV(c.EnumValues))
		}
	}
	for _, e := range r.data.Edges {
		fmt.Fprintf(&sb, "# join: %s -> %s (%s)\n", e.From, e.To, e.Cardinality)
	}
	for _, rule := range r.data.Usage.Rules {
		fmt.Fprintf(&sb, "# rule: %s\n", rule)
	}
	for _, q := range r.data.Usage.ExampleQueries {
		fmt.Fprintf(&sb, "# example: %s\n", q)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// enumPreviewLimit caps the number of enum names shown in the table-mode
// Notes column before collapsing the rest into a count. JSON output keeps
// the full list for agent consumption.
const enumPreviewLimit = 3

func (r *schemaResult) RenderTable() string {
	if len(r.data.Tables) == 0 && len(r.data.Edges) == 0 &&
		len(r.data.Usage.Rules) == 0 && len(r.data.Usage.ExampleQueries) == 0 {
		return "no tables"
	}

	var sb strings.Builder

	for i, tbl := range r.data.Tables {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(renderSchemaTable(tbl))
	}

	if len(r.data.Edges) > 0 {
		sb.WriteString("\n\nJoins\n")
		t := table.New().Headers("From", "To", "Cardinality")
		rows := make([][]string, 0, len(r.data.Edges))
		for _, e := range r.data.Edges {
			rows = append(rows, []string{e.From, e.To, e.Cardinality})
		}
		sb.WriteString(t.Rows(rows...).Render())
	}

	if len(r.data.Usage.Rules) > 0 || len(r.data.Usage.ExampleQueries) > 0 {
		sb.WriteString("\n\nUsage")
		for _, rule := range r.data.Usage.Rules {
			fmt.Fprintf(&sb, "\n- %s", rule)
		}
		if len(r.data.Usage.ExampleQueries) > 0 {
			sb.WriteString("\n\nExamples")
			for _, q := range r.data.Usage.ExampleQueries {
				fmt.Fprintf(&sb, "\n  %s", q)
			}
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func renderSchemaTable(tbl cloudquery.SchemaTable) string {
	var sb strings.Builder
	sb.WriteString(tbl.Name)
	if tbl.Description != "" {
		fmt.Fprintf(&sb, "  %s", tbl.Description)
	}
	if tbl.TimeColumn != "" {
		fmt.Fprintf(&sb, "  [time: %s", tbl.TimeColumn)
		if tbl.TimeWindowMaxDays > 0 {
			fmt.Fprintf(&sb, ", max %dd", tbl.TimeWindowMaxDays)
		}
		sb.WriteString("]")
	}
	sb.WriteString("\n")

	t := table.New().Headers("Column", "Type", "Caps", "Notes")
	rows := make([][]string, 0, len(tbl.Columns))
	for _, c := range tbl.Columns {
		rows = append(rows, []string{c.Name, c.Type, shortCaps(c), notesFor(c)})
	}
	sb.WriteString(t.Rows(rows...).Render())

	refs := referenceFootnotes(tbl)
	if len(refs) > 0 {
		sb.WriteString("\nrefs:")
		for _, line := range refs {
			fmt.Fprintf(&sb, "\n  %s", line)
		}
	}
	return sb.String()
}

func shortCaps(c cloudquery.SchemaColumn) string {
	flags := make([]string, 0, 5)
	if c.Selectable {
		flags = append(flags, "sel")
	}
	if c.Filterable {
		flags = append(flags, "fil")
	}
	if c.Groupable {
		flags = append(flags, "grp")
	}
	if c.Aggregatable {
		flags = append(flags, "agg")
	}
	if c.Indexed {
		flags = append(flags, "idx")
	}
	if len(flags) == 0 {
		return "-"
	}
	return strings.Join(flags, ",")
}

// notesFor returns the table-mode Notes cell: a truncated enum preview for
// enum columns, empty otherwise. Reference URLs are emitted separately as
// per-table footnotes by referenceFootnotes.
func notesFor(c cloudquery.SchemaColumn) string {
	if len(c.EnumValues) == 0 {
		return ""
	}
	if len(c.EnumValues) <= enumPreviewLimit {
		return enumNamesCSV(c.EnumValues)
	}
	shown := enumNamesCSV(c.EnumValues[:enumPreviewLimit])
	return fmt.Sprintf("%s (+%d more)", shown, len(c.EnumValues)-enumPreviewLimit)
}

func referenceFootnotes(tbl cloudquery.SchemaTable) []string {
	var lines []string
	for _, c := range tbl.Columns {
		if c.ReferenceURL != "" {
			lines = append(lines, fmt.Sprintf("%s -> %s", c.Name, c.ReferenceURL))
		}
	}
	return lines
}

// columnFlags is the long-form caps string retained for plain-mode output.
// Plain mode is a machine contract (TSV) and keeps the verbose flag names.
func columnFlags(c cloudquery.SchemaColumn) string {
	flags := []string{}
	if c.Selectable {
		flags = append(flags, "selectable")
	}
	if c.Filterable {
		flags = append(flags, "filterable")
	}
	if c.Groupable {
		flags = append(flags, "groupable")
	}
	if c.Aggregatable {
		flags = append(flags, "aggregatable")
	}
	if c.Indexed {
		flags = append(flags, "indexed")
	}
	if len(flags) == 0 {
		return "-"
	}
	return strings.Join(flags, ",")
}

func enumNamesCSV(vs []cloudquery.EnumValue) string {
	if len(vs) == 0 {
		return ""
	}
	names := make([]string, 0, len(vs))
	for _, v := range vs {
		names = append(names, v.Name)
	}
	return strings.Join(names, ",")
}
