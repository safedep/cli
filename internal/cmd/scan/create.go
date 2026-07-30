package scan

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

const maxProjectScans = 100

type createdScan struct {
	ProjectID     string
	ScanSessionID string
	Status        string
	CreatedAt     *time.Time
}

type createResult struct {
	scans []createdScan
}

type createdScanJSON struct {
	ProjectID     string `json:"project_id"`
	ScanSessionID string `json:"scan_session_id"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at,omitempty"`
}

type createResultJSON struct {
	Scans []createdScanJSON `json:"scans"`
}

func createCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "create PROJECT_ID [PROJECT_ID...]",
		Short: "Submit on-demand project scans",
		Long:  "Atomically submit on-demand scans for one to 100 SafeDep projects and return after admission.",
		Args: func(_ *cobra.Command, args []string) error {
			return validateProjectIDs(args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			result, err := createProjectScans(
				cmd.Context(),
				newCreateProjectScansClient(client.Connection()),
				args,
			)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
}

func validateProjectIDs(projectIDs []string) error {
	if len(projectIDs) < 1 || len(projectIDs) > maxProjectScans {
		return fmt.Errorf("scan create requires between 1 and %d project IDs", maxProjectScans)
	}

	seen := make(map[string]struct{}, len(projectIDs))
	for i, projectID := range projectIDs {
		if projectID == "" {
			return fmt.Errorf("project ID at position %d must not be empty", i+1)
		}
		if _, ok := seen[projectID]; ok {
			return fmt.Errorf("duplicate project ID %q", projectID)
		}
		seen[projectID] = struct{}{}
	}
	return nil
}

func (r *createResult) RenderJSON() ([]byte, error) {
	scans := make([]createdScanJSON, 0, len(r.scans))
	for _, scan := range r.scans {
		item := createdScanJSON{
			ProjectID:     scan.ProjectID,
			ScanSessionID: scan.ScanSessionID,
			Status:        scan.Status,
		}
		if scan.CreatedAt != nil {
			item.CreatedAt = scan.CreatedAt.UTC().Format(time.RFC3339)
		}
		scans = append(scans, item)
	}
	return json.MarshalIndent(createResultJSON{Scans: scans}, "", "  ")
}

func (r *createResult) RenderPlain() string {
	var output strings.Builder
	output.WriteString("project_id\tscan_session_id\tstatus\tcreated_at")
	for _, scan := range r.scans {
		output.WriteByte('\n')
		output.WriteString(strings.Join(scanCells(scan), "\t"))
	}
	return output.String()
}

func (r *createResult) RenderTable() string {
	rows := make([][]string, 0, len(r.scans))
	for _, scan := range r.scans {
		rows = append(rows, scanCells(scan))
	}
	return table.New().
		Headers("PROJECT ID", "SCAN SESSION ID", "STATUS", "CREATED AT").
		Rows(rows...).
		Render()
}

func scanCells(scan createdScan) []string {
	createdAt := ""
	if scan.CreatedAt != nil {
		createdAt = scan.CreatedAt.UTC().Format(time.RFC3339)
	}
	return []string{scan.ProjectID, scan.ScanSessionID, scan.Status, createdAt}
}
