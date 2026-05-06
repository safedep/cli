package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudquery"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

// Bounds match the buf.validate constraints in the QueryService proto
// at safedep-api/proto/safedep/services/controltower/v1/query.proto.
// The server enforces these via a validation interceptor; mirroring
// them client-side surfaces clearer errors than the wrapped
// "invalid argument" gRPC response.
const (
	defaultPageSize  = 100
	maxPageSize      = 100
	maxSQLBytes      = 2000
	maxPageTokenSize = 100
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

type execJSON struct {
	Columns       []string         `json:"columns"`
	Rows          []map[string]any `json:"rows"`
	Count         int              `json:"count"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

func (r *execResult) RenderJSON() ([]byte, error) {
	rows := make([]map[string]any, 0, len(r.data.Rows))
	for _, row := range r.data.Rows {
		rows = append(rows, map[string]any(row))
	}
	out := execJSON{
		Columns:       r.data.Columns,
		Rows:          rows,
		Count:         len(r.data.Rows),
		NextPageToken: r.data.NextPage,
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *execResult) RenderPlain() string {
	if len(r.data.Rows) == 0 {
		return "no rows"
	}

	var sb strings.Builder
	sb.WriteString(strings.Join(r.data.Columns, "\t"))
	sb.WriteString("\n")
	for _, row := range r.data.Rows {
		cells := make([]string, len(r.data.Columns))
		for i, col := range r.data.Columns {
			cells[i] = formatCell(row[col])
		}
		sb.WriteString(strings.Join(cells, "\t"))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *execResult) RenderTable() string {
	if len(r.data.Rows) == 0 {
		return "no rows"
	}

	t := table.New().Headers(r.data.Columns...)
	rows := make([][]string, 0, len(r.data.Rows))
	for _, row := range r.data.Rows {
		cells := make([]string, len(r.data.Columns))
		for i, col := range r.data.Columns {
			cells[i] = formatCell(row[col])
		}
		rows = append(rows, cells)
	}
	t = t.Rows(rows...)
	return t.Render()
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
