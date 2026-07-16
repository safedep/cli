package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudquery"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

// Bounds match the buf.validate constraints in the v2 QueryService proto.
// The server enforces these via a validation interceptor; mirroring them
// client-side surfaces clearer errors than the wrapped "invalid argument"
// gRPC response.
const (
	defaultPageSize  = 100
	maxPageSize      = 100
	maxSQLBytes      = 16000
	maxPageTokenSize = 2048
)

type execInput struct {
	SQL       string
	SQLFile   string
	PageSize  int
	PageToken string
}

func execCmd(a *app.App) *cobra.Command {
	in := execInput{PageSize: defaultPageSize}

	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Execute a SQL query against SafeDep Cloud",
		Long: "Execute a single SQL statement against SafeDep Cloud's query service " +
			"and print the rows. Reads the statement from --sql, --sql-file, or stdin (in that order).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			sql, err := resolveSQL(cmd.InOrStdin(), in)
			if err != nil {
				return err
			}

			pageSize, err := normalisePageSize(in.PageSize)
			if err != nil {
				return err
			}

			pageToken, err := validatePageToken(in.PageToken)
			if err != nil {
				return err
			}

			svc := cloudquery.NewService(client.Connection())
			result, err := runExec(cmd.Context(), svc, cloudquery.ExecInput{
				SQL:       sql,
				PageSize:  pageSize,
				PageToken: pageToken,
			})
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&in.SQL, "sql", "s", "", "SQL statement to execute (overrides --sql-file and stdin)")
	f.StringVar(&in.SQLFile, "sql-file", "", "path to a file containing the SQL statement")
	f.IntVar(&in.PageSize, "limit", defaultPageSize, "maximum rows to return (1-100)")
	f.StringVar(&in.PageToken, "page-token", "", "next_page_token from a prior response to continue paging")

	return cmd
}

// runExec is the orchestration shim. It accepts the small ExecRunner
// interface so unit tests pass stubs without touching gRPC.
func runExec(ctx context.Context, runner cloudquery.ExecRunner, in cloudquery.ExecInput) (*execResult, error) {
	res, err := runner.Exec(ctx, in)
	if err != nil {
		return nil, err
	}
	return &execResult{data: res}, nil
}

type execResult struct {
	data *cloudquery.ExecResult
}

type execColumnJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type execStatsJSON struct {
	EstimatedCost float64 `json:"estimated_cost"`
	EstimatedRows int64   `json:"estimated_rows"`
	ElapsedMs     int64   `json:"elapsed_ms"`
}

type execJSON struct {
	Columns       []execColumnJSON `json:"columns"`
	Rows          []map[string]any `json:"rows"`
	Count         int              `json:"count"`
	NextPageToken string           `json:"next_page_token,omitempty"`
	GeneratedAt   string           `json:"generated_at,omitempty"`
	Stats         execStatsJSON    `json:"stats"`
}

func (r *execResult) RenderJSON() ([]byte, error) {
	cols := make([]execColumnJSON, 0, len(r.data.Columns))
	for _, c := range r.data.Columns {
		cols = append(cols, execColumnJSON{Name: c.Name, Type: c.Type})
	}

	rows := make([]map[string]any, 0, len(r.data.Rows))
	for _, row := range r.data.Rows {
		rows = append(rows, map[string]any(row))
	}

	out := execJSON{
		Columns:       cols,
		Rows:          rows,
		Count:         len(r.data.Rows),
		NextPageToken: r.data.NextPage,
		Stats: execStatsJSON{
			EstimatedCost: r.data.Stats.EstimatedCost,
			EstimatedRows: r.data.Stats.EstimatedRows,
			ElapsedMs:     r.data.Stats.ElapsedMs,
		},
	}
	if !r.data.GeneratedAt.IsZero() {
		out.GeneratedAt = r.data.GeneratedAt.UTC().Format(time.RFC3339)
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *execResult) RenderPlain() string {
	if len(r.data.Rows) == 0 {
		return "no rows"
	}

	names := columnNames(r.data.Columns)
	var sb strings.Builder
	sb.WriteString(strings.Join(names, "\t"))
	sb.WriteString("\n")
	for _, row := range r.data.Rows {
		cells := make([]string, len(names))
		for i, col := range names {
			cells[i] = formatCell(row[col])
		}
		sb.WriteString(strings.Join(cells, "\t"))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *execResult) RenderTable() string {
	if len(r.data.Rows) == 0 {
		return section.Empty("no rows")
	}

	names := columnNames(r.data.Columns)
	rows := make([][]string, 0, len(r.data.Rows))
	for _, row := range r.data.Rows {
		cells := make([]string, len(names))
		for i, col := range names {
			cells[i] = formatCell(row[col])
		}
		rows = append(rows, cells)
	}

	return table.New().
		Headers(names...).
		Rows(rows...).
		Footer(renderExecFooter(r.data)).
		Render()
}

func columnNames(cols []cloudquery.Column) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

// renderExecFooter returns the D2 footer: one summary line, and a second
// line advertising the next-page cursor when present.
func renderExecFooter(r *cloudquery.ExecResult) string {
	rows := "rows"
	if len(r.Rows) == 1 {
		rows = "row"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d %s | ~%g cost | %dms",
		len(r.Rows), rows, r.Stats.EstimatedCost, r.Stats.ElapsedMs)
	if r.NextPage != "" {
		fmt.Fprintf(&sb, "\nnext page: --page-token %s", r.NextPage)
	}
	return sb.String()
}

func formatCell(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// Drop the trailing ".0" common to integer-valued JSON numbers.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprint(x)
	}
}
