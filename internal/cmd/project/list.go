package project

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
)

const (
	// unsetCount marks a count the server did not compute, which is not the
	// same as a computed zero.
	unsetCount = "-"
)

type listInput struct {
	Projects        []string
	ProjectVersions []string
	Status          string
	Trigger         string
	PageSize        uint32
	PageToken       string
}

type listedScan struct {
	ScanSessionID      string
	ProjectID          string
	ProjectName        string
	ProjectVersion     string
	Status             string
	Trigger            string
	CreatedAt          *time.Time
	Vulnerabilities    *uint32
	PolicyViolations   *uint32
	SuspiciousPackages *uint32
}

type listResult struct {
	scans         []listedScan
	nextPageToken string
}

type listedScanJSON struct {
	ScanSessionID      string  `json:"scan_session_id"`
	ScanURL            string  `json:"scan_url"`
	ProjectID          string  `json:"project_id,omitempty"`
	ProjectName        string  `json:"project_name,omitempty"`
	ProjectVersion     string  `json:"project_version,omitempty"`
	Status             string  `json:"status"`
	Trigger            string  `json:"trigger"`
	CreatedAt          string  `json:"created_at,omitempty"`
	Vulnerabilities    *uint32 `json:"vulnerabilities,omitempty"`
	PolicyViolations   *uint32 `json:"policy_violations,omitempty"`
	SuspiciousPackages *uint32 `json:"suspicious_packages,omitempty"`
}

type listResultJSON struct {
	Scans         []listedScanJSON `json:"scans"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

func listCmd(a *app.App) *cobra.Command {
	var in listInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List project scans performed by SafeDep Cloud-hosted scanners",
		Long: "List scan sessions run for the active tenant by SafeDep Cloud-hosted scanners. Filter " +
			"by exact project name, project version, scan status, or scan trigger. One page is " +
			"returned per invocation, so follow --page-token to read the next page.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			result, err := listProjectScans(cmd.Context(), newListScansClient(client.Connection()), &in)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&in.Projects, "project", nil,
		fmt.Sprintf("filter: exact project name; repeat for up to %d projects", maxScanFilterValues))
	f.StringArrayVar(&in.ProjectVersions, "project-version", nil,
		fmt.Sprintf("filter: exact project version; repeat for up to %d versions", maxScanFilterValues))
	f.StringVar(&in.Status, "status", "",
		"filter: scan status ("+strings.Join(scanStatusTokens(), ", ")+")")
	f.StringVar(&in.Trigger, "trigger", "",
		"filter: scan trigger ("+strings.Join(scanTriggerTokens(), ", ")+")")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func (r *listResult) RenderJSON() ([]byte, error) {
	scans := make([]listedScanJSON, 0, len(r.scans))
	for _, scan := range r.scans {
		item := listedScanJSON{
			ScanSessionID:      scan.ScanSessionID,
			ScanURL:            scanURL(scan.ScanSessionID),
			ProjectID:          scan.ProjectID,
			ProjectName:        scan.ProjectName,
			ProjectVersion:     scan.ProjectVersion,
			Status:             scan.Status,
			Trigger:            scan.Trigger,
			Vulnerabilities:    scan.Vulnerabilities,
			PolicyViolations:   scan.PolicyViolations,
			SuspiciousPackages: scan.SuspiciousPackages,
		}
		if scan.CreatedAt != nil {
			item.CreatedAt = scan.CreatedAt.UTC().Format(time.RFC3339)
		}
		scans = append(scans, item)
	}
	return json.MarshalIndent(listResultJSON{Scans: scans, NextPageToken: r.nextPageToken}, "", "  ")
}

func (r *listResult) RenderPlain() string {
	var output strings.Builder
	output.WriteString(
		"scan_session_id\tproject_name\tproject_version\tstatus\ttrigger\t" +
			"vulnerabilities\tpolicy_violations\tsuspicious_packages\tcreated_at\tscan_url",
	)
	for i := range r.scans {
		scan := &r.scans[i]
		cells := append(listedScanCells(scan, nil), scanURL(scan.ScanSessionID))
		output.WriteByte('\n')
		output.WriteString(strings.Join(cells, "\t"))
	}
	if r.nextPageToken != "" {
		output.WriteString("\nnext_page_token\t" + r.nextPageToken)
	}
	return output.String()
}

func (r *listResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.scans))
	for i := range r.scans {
		rows = append(rows, listedScanCells(&r.scans[i], &now))
	}

	t := table.New().
		Title("Project scans").
		Headers("SCAN SESSION ID", "PROJECT", "VERSION", "STATUS", "TRIGGER",
			"VULNS", "VIOLATIONS", "SUSPICIOUS", "CREATED").
		Rows(rows...).
		EmptyMessage("No project scans found. Submit one with: safedep project scan create <project-name>")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), pluralScans(len(rows)))
		if r.nextPageToken != "" {
			footer += ". More available: --page-token " + r.nextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

// listedScanCells renders one scan as display cells. A non-nil now switches the
// creation time to a humanized form for table output. The scan URL is appended
// by plain output only: it does not fit the table width.
func listedScanCells(scan *listedScan, now *time.Time) []string {
	created := ""
	switch {
	case scan.CreatedAt == nil:
	case now != nil:
		created = humanize.Time(*scan.CreatedAt, *now)
	default:
		created = scan.CreatedAt.UTC().Format(time.RFC3339)
	}
	return []string{
		scan.ScanSessionID,
		scan.ProjectName,
		scan.ProjectVersion,
		scan.Status,
		scan.Trigger,
		countCell(scan.Vulnerabilities),
		countCell(scan.PolicyViolations),
		countCell(scan.SuspiciousPackages),
		created,
	}
}

func countCell(count *uint32) string {
	if count == nil {
		return unsetCount
	}
	return strconv.FormatUint(uint64(*count), 10)
}

func pluralScans(n int) string {
	if n == 1 {
		return "scan"
	}
	return "scans"
}
