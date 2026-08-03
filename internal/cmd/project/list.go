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

// listPlainHeaders is the plain output header, and the field order every plain
// row follows.
var listPlainHeaders = []string{
	"scan_session_id",
	"project_id",
	"project_name",
	"project_version",
	"status",
	"trigger",
	"vulnerabilities",
	"policy_violations",
	"suspicious_packages",
	"created_at",
	"scan_url",
}

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
		// Validation belongs here rather than in RunE: cobra runs it before RunE
		// reaches the credential store, so a flag typo never costs a keychain read
		// or a token refresh. This matches create and sync.
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validateListInput(&in)
		},
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
	output.WriteString(strings.Join(listPlainHeaders, "\t"))
	for i := range r.scans {
		output.WriteByte('\n')
		output.WriteString(strings.Join(listedScanPlainCells(&r.scans[i]), "\t"))
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

// listedScanCells renders one scan as table cells. The table drops project_id
// and the scan URL that plain output carries: eleven columns, two of them long
// opaque identifiers, do not fit a terminal.
func listedScanCells(scan *listedScan, now *time.Time) []string {
	return []string{
		scan.ScanSessionID,
		scan.ProjectName,
		scan.ProjectVersion,
		scan.Status,
		scan.Trigger,
		countCell(scan.Vulnerabilities),
		countCell(scan.PolicyViolations),
		countCell(scan.SuspiciousPackages),
		createdAtCell(scan.CreatedAt, now),
	}
}

// listedScanPlainCells renders one scan for shell pipelines. It carries every
// field, in the order of listPlainHeaders, so a consumer can cut any column.
func listedScanPlainCells(scan *listedScan) []string {
	return []string{
		scan.ScanSessionID,
		scan.ProjectID,
		scan.ProjectName,
		scan.ProjectVersion,
		scan.Status,
		scan.Trigger,
		countCell(scan.Vulnerabilities),
		countCell(scan.PolicyViolations),
		countCell(scan.SuspiciousPackages),
		createdAtCell(scan.CreatedAt, nil),
		scanURL(scan.ScanSessionID),
	}
}

// createdAtCell formats a creation time. A non-nil now switches it to a
// humanized form for table output.
func createdAtCell(createdAt *time.Time, now *time.Time) string {
	switch {
	case createdAt == nil:
		return ""
	case now != nil:
		return humanize.Time(*createdAt, *now)
	default:
		return createdAt.UTC().Format(time.RFC3339)
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
