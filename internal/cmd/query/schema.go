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
	return &cobra.Command{
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
			result, err := runSchema(cmd.Context(), svc)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
}

func runSchema(ctx context.Context, fetcher cloudquery.SchemaFetcher) (*schemaResult, error) {
	res, err := fetcher.Schema(ctx)
	if err != nil {
		return nil, err
	}
	return &schemaResult{data: sortSchema(res)}, nil
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

func (r *schemaResult) RenderTable() string {
	if len(r.data.Tables) == 0 && len(r.data.Edges) == 0 &&
		len(r.data.Usage.Rules) == 0 && len(r.data.Usage.ExampleQueries) == 0 {
		return "no tables"
	}

	var sb strings.Builder

	if len(r.data.Tables) > 0 {
		t := table.New().Headers("Table", "Column", "Type", "Caps", "Enum", "Reference")
		rows := make([][]string, 0)
		for _, tbl := range r.data.Tables {
			for _, c := range tbl.Columns {
				rows = append(rows, []string{
					tbl.Name,
					c.Name,
					c.Type,
					columnFlags(c),
					enumNamesCSV(c.EnumValues),
					c.ReferenceURL,
				})
			}
		}
		sb.WriteString(t.Rows(rows...).Render())
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
