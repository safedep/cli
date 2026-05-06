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
		Long:  "Fetch the SQL schema served by SafeDep Cloud, listing each table and its columns with selectability and reference URLs.",
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
// output is deterministic across runs and presentation modes.
func sortSchema(s *cloudquery.Schema) *cloudquery.Schema {
	out := &cloudquery.Schema{Tables: make([]cloudquery.SchemaTable, len(s.Tables))}
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
}

type schemaTableJSON struct {
	Name    string             `json:"name"`
	Columns []schemaColumnJSON `json:"columns"`
}

type schemaColumnJSON struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Selectable   bool   `json:"selectable"`
	Filterable   bool   `json:"filterable"`
	Required     bool   `json:"required"`
	ReferenceURL string `json:"reference_url,omitempty"`
}

func (r *schemaResult) RenderJSON() ([]byte, error) {
	out := schemaJSON{Tables: make([]schemaTableJSON, 0, len(r.data.Tables))}
	for _, tbl := range r.data.Tables {
		cols := make([]schemaColumnJSON, 0, len(tbl.Columns))
		for _, c := range tbl.Columns {
			cols = append(cols, schemaColumnJSON{
				Name:         c.Name,
				Description:  c.Description,
				Selectable:   c.Selectable,
				Filterable:   c.Filterable,
				Required:     c.Required,
				ReferenceURL: c.ReferenceURL,
			})
		}
		out.Tables = append(out.Tables, schemaTableJSON{Name: tbl.Name, Columns: cols})
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *schemaResult) RenderPlain() string {
	if len(r.data.Tables) == 0 {
		return "no tables"
	}

	var sb strings.Builder
	for _, tbl := range r.data.Tables {
		for _, c := range tbl.Columns {
			fmt.Fprintf(&sb, "%s.%s\t%s\t%s\n", tbl.Name, c.Name, columnFlags(c), c.ReferenceURL)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *schemaResult) RenderTable() string {
	if len(r.data.Tables) == 0 {
		return "no tables"
	}

	t := table.New().Headers("Table", "Column", "Selectable", "Filterable", "Reference")
	rows := make([][]string, 0)
	for _, tbl := range r.data.Tables {
		for _, c := range tbl.Columns {
			rows = append(rows, []string{
				tbl.Name,
				c.Name,
				yesNo(c.Selectable),
				yesNo(c.Filterable),
				c.ReferenceURL,
			})
		}
	}
	return t.Rows(rows...).Render()
}

func columnFlags(c cloudquery.SchemaColumn) string {
	flags := []string{}
	if c.Selectable {
		flags = append(flags, "selectable")
	}
	if c.Filterable {
		flags = append(flags, "filterable")
	}
	if c.Required {
		flags = append(flags, "required")
	}
	if len(flags) == 0 {
		return "-"
	}
	return strings.Join(flags, ",")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
