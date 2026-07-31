package project

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

const (
	maxProjectScans = 100
	scanURLBase     = "https://app.safedep.io/scans/"
)

type createInput struct {
	ProjectIDs   []string
	ProjectNames []string
}

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
	ScanURL       string `json:"scan_url"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at,omitempty"`
}

type createResultJSON struct {
	Scans []createdScanJSON `json:"scans"`
}

func createCmd(a *app.App) *cobra.Command {
	var in createInput
	cmd := &cobra.Command{
		Use:   "create [PROJECT_NAME...]",
		Short: "Submit project scans to SafeDep Cloud-hosted scanners",
		Long: "Atomically submit on-demand scans for one to 100 SafeDep projects by exact name or ID " +
			"to SafeDep Cloud-hosted scanners. The command returns after cloud admission while scan " +
			"execution continues asynchronously in SafeDep Cloud.",
		Args: func(_ *cobra.Command, args []string) error {
			in.ProjectNames = args
			return validateCreateInput(in)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			in.ProjectNames = args
			result, err := runCreate(
				cmd.Context(),
				newCreateProjectScansClient(client.Connection()),
				newListProjectsClient(client.Connection()),
				in,
			)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
	cmd.Flags().StringArrayVar(
		&in.ProjectIDs,
		"project-id",
		nil,
		"project ID to scan instead of a name; repeat for multiple projects",
	)
	return cmd
}

func validateCreateInput(in createInput) error {
	total := len(in.ProjectIDs) + len(in.ProjectNames)
	if total < 1 || total > maxProjectScans {
		return fmt.Errorf("project scan create requires between 1 and %d projects", maxProjectScans)
	}
	if err := validateUniqueValues(in.ProjectIDs, "project ID"); err != nil {
		return err
	}
	return validateUniqueValues(in.ProjectNames, "project name")
}

func validateProjectIDs(projectIDs []string) error {
	if len(projectIDs) < 1 || len(projectIDs) > maxProjectScans {
		return fmt.Errorf("project scan create requires between 1 and %d project IDs", maxProjectScans)
	}
	return validateUniqueValues(projectIDs, "project ID")
}

func validateUniqueValues(values []string, label string) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if value == "" {
			return fmt.Errorf("%s at position %d must not be empty", label, i+1)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (r *createResult) RenderJSON() ([]byte, error) {
	scans := make([]createdScanJSON, 0, len(r.scans))
	for _, scan := range r.scans {
		item := createdScanJSON{
			ProjectID:     scan.ProjectID,
			ScanSessionID: scan.ScanSessionID,
			ScanURL:       scanURL(scan.ScanSessionID),
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
	output.WriteString("project_id\tscan_session_id\tscan_url\tstatus\tcreated_at")
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
		Headers("PROJECT ID", "SCAN SESSION ID", "SCAN URL", "STATUS", "CREATED AT").
		Rows(rows...).
		Render()
}

func scanCells(scan createdScan) []string {
	createdAt := ""
	if scan.CreatedAt != nil {
		createdAt = scan.CreatedAt.UTC().Format(time.RFC3339)
	}
	return []string{
		scan.ProjectID,
		scan.ScanSessionID,
		scanURL(scan.ScanSessionID),
		scan.Status,
		createdAt,
	}
}

func scanURL(scanSessionID string) string {
	return scanURLBase + scanSessionID
}
