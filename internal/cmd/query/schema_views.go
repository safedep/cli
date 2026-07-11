package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudquery"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

// schemaListCmd is the table-of-tables view. Use it for orientation; reach
// for schema show <table> to drill in, or schema get -o json for the full
// machine-readable schema.
func schemaListCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tables in the SafeDep Cloud query schema",
		Long: "List the queryable tables with column counts and descriptions. " +
			"Use 'safedep query schema show <table>' to inspect one table; " +
			"'safedep query schema get -o json' for the full machine-readable schema.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			svc := cloudquery.NewService(client.Connection())
			result, err := runSchemaList(cmd.Context(), svc)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
}

func runSchemaList(ctx context.Context, fetcher cloudquery.SchemaFetcher) (*schemaListResult, error) {
	res, err := fetcher.Schema(ctx)
	if err != nil {
		return nil, err
	}
	return &schemaListResult{data: sortSchema(res)}, nil
}

type schemaListResult struct {
	data *cloudquery.Schema
}

type schemaListJSON struct {
	Tables []schemaListEntryJSON `json:"tables"`
}

type schemaListEntryJSON struct {
	Name        string `json:"name"`
	Columns     int    `json:"columns"`
	Description string `json:"description,omitempty"`
}

func (r *schemaListResult) RenderJSON() ([]byte, error) {
	out := schemaListJSON{Tables: make([]schemaListEntryJSON, 0, len(r.data.Tables))}
	for _, tbl := range r.data.Tables {
		out.Tables = append(out.Tables, schemaListEntryJSON{
			Name:        tbl.Name,
			Columns:     len(tbl.Columns),
			Description: tbl.Description,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *schemaListResult) RenderPlain() string {
	if len(r.data.Tables) == 0 {
		return "no tables"
	}
	var sb strings.Builder
	for _, tbl := range r.data.Tables {
		fmt.Fprintf(&sb, "%s\t%d\t%s\n", tbl.Name, len(tbl.Columns), tbl.Description)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *schemaListResult) RenderTable() string {
	if len(r.data.Tables) == 0 {
		return section.Empty("no tables")
	}
	rows := make([][]string, 0, len(r.data.Tables))
	for _, tbl := range r.data.Tables {
		rows = append(rows, []string{tbl.Name, fmt.Sprintf("%d", len(tbl.Columns)), tbl.Description})
	}
	return section.Join(
		table.New().
			Title("Query schema").
			Headers("Table", "Columns", "Description").
			Rows(rows...).
			Footer(fmt.Sprintf("%d tables", len(r.data.Tables))).
			Render(),
		section.Hint("Use 'safedep query schema show <table>' to inspect one table."),
	)
}

// schemaShowCmd renders a single table in depth, including joins that
// involve it in either direction.
func schemaShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <table>",
		Short: "Show one table from the SafeDep Cloud query schema",
		Long: "Show a single table's columns, capability flags, enum values, " +
			"and joins involving it. For the full schema use 'safedep query schema get'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			svc := cloudquery.NewService(client.Connection())
			result, err := runSchemaShow(cmd.Context(), svc, args[0])
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
}

func runSchemaShow(ctx context.Context, fetcher cloudquery.SchemaFetcher, name string) (*schemaShowResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("table name is required")
	}

	res, err := fetcher.Schema(ctx)
	if err != nil {
		return nil, err
	}
	sorted := sortSchema(res)

	var tbl *cloudquery.SchemaTable
	for i := range sorted.Tables {
		if sorted.Tables[i].Name == name {
			tbl = &sorted.Tables[i]
			break
		}
	}
	if tbl == nil {
		names := make([]string, 0, len(sorted.Tables))
		for _, t := range sorted.Tables {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unknown table %q (available: %s)", name, strings.Join(names, ", "))
	}

	edges := edgesInvolving(sorted.Edges, name)
	return &schemaShowResult{table: *tbl, edges: edges}, nil
}

// edgesInvolving returns the subset of edges where the table appears at
// either endpoint. schema get --table uses both-endpoints filtering; schema
// show wants either-endpoint because a single-table view is about discovery.
func edgesInvolving(edges []cloudquery.JoinEdge, name string) []cloudquery.JoinEdge {
	var out []cloudquery.JoinEdge
	for _, e := range edges {
		if e.From == name || e.To == name {
			out = append(out, e)
		}
	}
	return out
}

type schemaShowResult struct {
	table cloudquery.SchemaTable
	edges []cloudquery.JoinEdge
}

type schemaShowJSON struct {
	Table schemaTableJSON  `json:"table"`
	Edges []schemaEdgeJSON `json:"edges,omitempty"`
}

func (r *schemaShowResult) RenderJSON() ([]byte, error) {
	cols := make([]schemaColumnJSON, 0, len(r.table.Columns))
	for _, c := range r.table.Columns {
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
	out := schemaShowJSON{
		Table: schemaTableJSON{
			Name:              r.table.Name,
			Description:       r.table.Description,
			Columns:           cols,
			TimeColumn:        r.table.TimeColumn,
			TimeWindowMaxDays: r.table.TimeWindowMaxDays,
		},
	}
	if len(r.edges) > 0 {
		out.Edges = make([]schemaEdgeJSON, 0, len(r.edges))
		for _, e := range r.edges {
			out.Edges = append(out.Edges, schemaEdgeJSON{From: e.From, To: e.To, Cardinality: e.Cardinality})
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *schemaShowResult) RenderPlain() string {
	var sb strings.Builder
	for _, c := range r.table.Columns {
		fmt.Fprintf(&sb, "%s.%s\t%s\t%s\t%s\n",
			r.table.Name, c.Name, c.Type, columnFlags(c), enumNamesCSV(c.EnumValues))
	}
	for _, e := range r.edges {
		fmt.Fprintf(&sb, "# join: %s -> %s (%s)\n", e.From, e.To, e.Cardinality)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *schemaShowResult) RenderTable() string {
	parts := []string{renderSchemaTable(r.table)}
	if len(r.edges) > 0 {
		var sb strings.Builder
		for _, e := range r.edges {
			if e.Cardinality != "" {
				fmt.Fprintf(&sb, "- %s -> %s (%s)\n", e.From, e.To, e.Cardinality)
				continue
			}
			fmt.Fprintf(&sb, "- %s -> %s\n", e.From, e.To)
		}
		parts = append(parts, section.Titled("Joins", strings.TrimRight(sb.String(), "\n")))
	}
	return section.Join(parts...)
}
