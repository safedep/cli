package project

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeListScansClient struct {
	calls int
	req   *controltowerv1.ListScansRequest
	res   *controltowerv1.ListScansResponse
	err   error
}

func (f *fakeListScansClient) ListScans(
	_ context.Context,
	req *controltowerv1.ListScansRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListScansResponse, error) {
	f.calls++
	f.req = req
	return f.res, f.err
}

var (
	_ listScansClient = (*fakeListScansClient)(nil)
	_ listScansClient = controltowerv1grpc.ScanServiceClient(nil)
)

func TestListCmd_TakesNoPositionalArguments(t *testing.T) {
	t.Parallel()

	cmd := listCmd(nil)

	assert.Equal(t, "list", cmd.Use)
	require.NoError(t, cmd.Args(cmd, nil))
	require.Error(t, cmd.Args(cmd, []string{"safedep/cli"}))

	for _, name := range []string{"project", "project-version", "status", "trigger", "limit", "page-token"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "expected --%s flag", name)
	}
}

func TestListCmd_FlagHelpListsEveryEnumToken(t *testing.T) {
	t.Parallel()

	cmd := listCmd(nil)

	status := cmd.Flags().Lookup("status")
	require.NotNil(t, status)
	assert.Equal(t, "filter: scan status (success, error, queued, running)", status.Usage)

	trigger := cmd.Flags().Lookup("trigger")
	require.NotNil(t, trigger)
	assert.Equal(t, "filter: scan trigger (push, pull-request, tag, manual, scheduled)", trigger.Usage)
}

// A nil App panics the moment RunE reaches a.ControlPlane, so an error rather
// than a panic proves local validation runs before any credential or network
// work.
func TestListCmd_ValidatesFlagsBeforeTouchingAuth(t *testing.T) {
	t.Parallel()

	cmd := listCmd(nil)
	cmd.SetArgs([]string{"--status", "bogus"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown --status value "bogus"`)
}

func TestListCmd_ArgsRejectsBothPositionalsAndBadFlags(t *testing.T) {
	t.Parallel()

	cmd := listCmd(nil)
	require.Error(t, cmd.Args(cmd, []string{"unexpected"}), "list takes no positional arguments")

	require.NoError(t, cmd.ParseFlags([]string{"--trigger", "cron"}))
	err := cmd.Args(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown --trigger value "cron"`)
}

func TestValidateListInput(t *testing.T) {
	t.Parallel()

	eleven := make([]string, maxScanFilterValues+1)
	for i := range eleven {
		eleven[i] = string(rune('a' + i))
	}

	tests := []struct {
		name    string
		in      listInput
		wantErr string
	}{
		{name: "empty"},
		{name: "filters", in: listInput{
			Projects:        []string{"project-1", "project-2"},
			ProjectVersions: []string{"main"},
			Status:          "queued",
			Trigger:         "manual",
		}},
		{name: "too many projects", in: listInput{Projects: eleven}, wantErr: "at most 10 project name"},
		{name: "too many versions", in: listInput{ProjectVersions: eleven}, wantErr: "at most 10 project version"},
		{
			name:    "duplicate project",
			in:      listInput{Projects: []string{"project-1", "project-1"}},
			wantErr: "duplicate project name",
		},
		{name: "empty project", in: listInput{Projects: []string{""}}, wantErr: "must not be empty"},
		{
			name: "project at the length limit",
			in:   listInput{Projects: []string{strings.Repeat("a", maxScanFilterValueLength)}},
		},
		{
			name:    "project over the length limit",
			in:      listInput{Projects: []string{strings.Repeat("a", maxScanFilterValueLength+1)}},
			wantErr: "over the 255 character limit",
		},
		{
			name:    "version over the length limit",
			in:      listInput{ProjectVersions: []string{strings.Repeat("a", maxScanFilterValueLength+1)}},
			wantErr: "over the 255 character limit",
		},
		{name: "unknown status", in: listInput{Status: "pending"}, wantErr: `unknown --status value "pending"`},
		{name: "unknown trigger", in: listInput{Trigger: "cron"}, wantErr: `unknown --trigger value "cron"`},
		{name: "unspecified status", in: listInput{Status: "unknown"}, wantErr: `unknown --status value "unknown"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateListInput(&tt.in)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestListProjectScans_OmitsFilterWhenNoSelectorIsSet(t *testing.T) {
	t.Parallel()

	client := &fakeListScansClient{res: &controltowerv1.ListScansResponse{}}
	_, err := listProjectScans(context.Background(), client, &listInput{})
	require.NoError(t, err)

	require.Equal(t, 1, client.calls)
	assert.False(t, client.req.HasFilter())
	assert.Zero(t, client.req.GetPagination().GetPageSize())
	assert.Empty(t, client.req.GetPagination().GetPageToken())
}

func TestListProjectScans_TranslatesFiltersAndPagination(t *testing.T) {
	t.Parallel()

	client := &fakeListScansClient{res: &controltowerv1.ListScansResponse{}}
	_, err := listProjectScans(context.Background(), client, &listInput{
		Projects:        []string{"project-1"},
		ProjectVersions: []string{"main"},
		Status:          "running",
		Trigger:         "pull-request",
		PageSize:        25,
		PageToken:       "token",
	})
	require.NoError(t, err)

	filter := client.req.GetFilter()
	require.NotNil(t, filter)
	assert.Equal(t, []string{"project-1"}, filter.GetProjects())
	assert.Equal(t, []string{"main"}, filter.GetProjectVersions())
	assert.Equal(t, messagescontroltowerv1.ScanStatus_SCAN_STATUS_RUNNING, filter.GetStatus())
	assert.Equal(t, messagescontroltowerv1.ScanTrigger_SCAN_TRIGGER_PULL_REQUEST, filter.GetTrigger())
	assert.Equal(t, uint32(25), client.req.GetPagination().GetPageSize())
	assert.Equal(t, "token", client.req.GetPagination().GetPageToken())
}

func TestListProjectScans_RejectsInvalidInputBeforeRPC(t *testing.T) {
	t.Parallel()

	client := &fakeListScansClient{}
	_, err := listProjectScans(context.Background(), client, &listInput{Status: "nope"})
	require.Error(t, err)
	assert.Zero(t, client.calls)
}

func TestListProjectScans_TranslatesScanSessions(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 15, 30, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	info := scanSessionInfo("session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_SUCCESS,
		messagescontroltowerv1.ScanTrigger_SCAN_TRIGGER_PUSH, createdAt)
	info.SetProject(scanSessionProject("project-1", "safedep/cli", "main"))
	info.SetVulnerabilities(3)
	info.SetPolicyViolations(0)

	client := &fakeListScansClient{res: listResponse("next", info)}
	result, err := listProjectScans(context.Background(), client, &listInput{})
	require.NoError(t, err)

	require.Len(t, result.scans, 1)
	scan := result.scans[0]
	assert.Equal(t, "session-1", scan.ScanSessionID)
	assert.Equal(t, "project-1", scan.ProjectID)
	assert.Equal(t, "safedep/cli", scan.ProjectName)
	assert.Equal(t, "main", scan.ProjectVersion)
	assert.Equal(t, "success", scan.Status)
	assert.Equal(t, "push", scan.Trigger)
	require.NotNil(t, scan.CreatedAt)
	assert.Equal(t, "2026-07-30T10:00:00Z", scan.CreatedAt.Format(time.RFC3339))
	require.NotNil(t, scan.Vulnerabilities)
	assert.Equal(t, uint32(3), *scan.Vulnerabilities)
	require.NotNil(t, scan.PolicyViolations)
	assert.Equal(t, uint32(0), *scan.PolicyViolations)
	assert.Nil(t, scan.SuspiciousPackages, "an unset count must stay unset rather than become zero")
	assert.Equal(t, "next", result.nextPageToken)
}

func TestListProjectScans_ToleratesMissingProjectAttributes(t *testing.T) {
	t.Parallel()

	info := scanSessionInfo("session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED,
		messagescontroltowerv1.ScanTrigger_SCAN_TRIGGER_UNSPECIFIED, time.Time{})

	client := &fakeListScansClient{res: listResponse("", info)}
	result, err := listProjectScans(context.Background(), client, &listInput{})
	require.NoError(t, err)

	require.Len(t, result.scans, 1)
	assert.Empty(t, result.scans[0].ProjectID)
	assert.Empty(t, result.scans[0].ProjectName)
	assert.Empty(t, result.scans[0].ProjectVersion)
	assert.Equal(t, "unknown", result.scans[0].Trigger)
	assert.Nil(t, result.scans[0].CreatedAt)
}

func TestListProjectScans_RejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		res     *controltowerv1.ListScansResponse
		wantErr string
	}{
		{
			name:    "missing scan session",
			res:     listResponse("", &controltowerv1.ScanSessionInfo{}),
			wantErr: "missing scan session",
		},
		{
			name: "missing session ID",
			res: listResponse("", func() *controltowerv1.ScanSessionInfo {
				info := &controltowerv1.ScanSessionInfo{}
				info.SetScanSession(&messagescontroltowerv1.ScanSession{})
				return info
			}()),
			wantErr: "missing scan session ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeListScansClient{res: tt.res}
			_, err := listProjectScans(context.Background(), client, &listInput{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "project scan list: invalid response")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestListProjectScans_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	client := &fakeListScansClient{err: status.Error(codes.PermissionDenied, "denied")}
	_, err := listProjectScans(context.Background(), client, &listInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project scan list")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestListResult_RenderJSON(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	vulnerabilities := uint32(2)
	result := &listResult{
		scans: []listedScan{
			{
				ScanSessionID:   "session-1",
				ProjectID:       "project-1",
				ProjectName:     "safedep/cli",
				ProjectVersion:  "main",
				Status:          "success",
				Trigger:         "push",
				CreatedAt:       &createdAt,
				Vulnerabilities: &vulnerabilities,
			},
			{ScanSessionID: "session-2", Status: "queued", Trigger: "manual"},
		},
		nextPageToken: "next",
	}

	got, err := result.RenderJSON()
	require.NoError(t, err)

	var parsed struct {
		Scans         []map[string]any `json:"scans"`
		NextPageToken string           `json:"next_page_token"`
	}
	require.NoError(t, json.Unmarshal(got, &parsed))
	require.Len(t, parsed.Scans, 2)
	assert.Equal(t, "session-1", parsed.Scans[0]["scan_session_id"])
	assert.Equal(t, "https://app.safedep.io/scans/session-1", parsed.Scans[0]["scan_url"])
	assert.Equal(t, "safedep/cli", parsed.Scans[0]["project_name"])
	assert.Equal(t, "2026-07-30T10:00:00Z", parsed.Scans[0]["created_at"])
	assert.InDelta(t, 2, parsed.Scans[0]["vulnerabilities"], 0)
	assert.NotContains(t, parsed.Scans[1], "created_at")
	assert.NotContains(t, parsed.Scans[1], "vulnerabilities")
	assert.NotContains(t, parsed.Scans[1], "project_name")
	assert.Equal(t, "next", parsed.NextPageToken)
}

func TestListResult_RenderPlain(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	violations := uint32(0)
	result := &listResult{
		scans: []listedScan{{
			ScanSessionID:    "session-1",
			ProjectID:        "project-1",
			ProjectName:      "safedep/cli",
			ProjectVersion:   "main",
			Status:           "success",
			Trigger:          "push",
			CreatedAt:        &createdAt,
			PolicyViolations: &violations,
		}},
		nextPageToken: "next",
	}

	lines := strings.Split(result.RenderPlain(), "\n")
	require.Len(t, lines, 2, "plain output is a header plus one line per scan, nothing else")
	assert.Equal(
		t,
		"scan_session_id\tproject_id\tproject_name\tproject_version\tstatus\ttrigger\t"+
			"vulnerabilities\tpolicy_violations\tsuspicious_packages\tcreated_at\tscan_url",
		lines[0],
	)
	assert.Equal(
		t,
		"session-1\tproject-1\tsafedep/cli\tmain\tsuccess\tpush\t-\t0\t-\t2026-07-30T10:00:00Z\t"+
			"https://app.safedep.io/scans/session-1",
		lines[1],
	)
}

// project_id is only reachable from plain and JSON output, so a pipeline can
// feed it to `project scan create --project-id`.
func TestListResult_RenderPlainCarriesProjectID(t *testing.T) {
	t.Parallel()

	result := &listResult{scans: []listedScan{{
		ScanSessionID: "session-1", ProjectID: "project-1", Status: "queued", Trigger: "manual",
	}}}

	lines := strings.Split(result.RenderPlain(), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "project_id", strings.Split(lines[0], "\t")[1])
	assert.Equal(t, "project-1", strings.Split(lines[1], "\t")[1])
}

// Every plain row must carry the same field count so a shell pipeline can cut
// columns. The continuation token lives in table and JSON output only.
func TestListResult_RenderPlainRowsAreUniformlyShaped(t *testing.T) {
	t.Parallel()

	result := &listResult{
		scans: []listedScan{
			{ScanSessionID: "session-1", Status: "queued", Trigger: "manual"},
			{ScanSessionID: "session-2", Status: "success", Trigger: "push"},
		},
		nextPageToken: "next",
	}

	lines := strings.Split(result.RenderPlain(), "\n")
	require.Len(t, lines, 3)
	for i, line := range lines {
		assert.Len(t, strings.Split(line, "\t"), len(listPlainHeaders), "line %d field count", i)
		assert.NotContains(t, line, "next_page_token")
	}
}

func TestListResult_RenderTable(t *testing.T) {
	t.Parallel()

	createdAt := time.Now().Add(-2 * time.Hour)
	result := &listResult{
		scans: []listedScan{{
			ScanSessionID:  "session-1",
			ProjectName:    "safedep/cli",
			ProjectVersion: "main",
			Status:         "success",
			Trigger:        "push",
			CreatedAt:      &createdAt,
		}},
		nextPageToken: "next",
	}

	got := result.RenderTable()
	for _, want := range []string{
		"SCAN SESSION ID", "PROJECT", "VERSION", "STATUS", "TRIGGER",
		"VULNS", "VIOLATIONS", "SUSPICIOUS", "CREATED",
		"session-1", "safedep/cli", "main", "success", "push",
		"1 scan", "--page-token next",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "https://app.safedep.io/scans/session-1",
		"the scan URL does not fit the table width")
}

func TestListResult_RenderTableIsEmptyMessageWhenNoScans(t *testing.T) {
	t.Parallel()

	got := (&listResult{}).RenderTable()
	assert.Contains(t, got, "No project scans found")
	assert.NotContains(t, got, "--page-token")
}

func listResponse(nextPageToken string, sessions ...*controltowerv1.ScanSessionInfo) *controltowerv1.ListScansResponse {
	res := &controltowerv1.ListScansResponse{}
	res.SetScanSessions(sessions)
	if nextPageToken != "" {
		pagination := &messagescontroltowerv1.PaginationResponse{}
		pagination.SetNextPageToken(nextPageToken)
		res.SetPagination(pagination)
	}
	return res
}

func scanSessionInfo(
	sessionID string,
	scanStatus messagescontroltowerv1.ScanStatus,
	trigger messagescontroltowerv1.ScanTrigger,
	createdAt time.Time,
) *controltowerv1.ScanSessionInfo {
	id := &messagescontroltowerv1.ScanSessionId{}
	id.SetSessionId(sessionID)
	session := &messagescontroltowerv1.ScanSession{}
	session.SetScanSessionId(id)
	session.SetStatus(scanStatus)
	session.SetTrigger(trigger)
	if !createdAt.IsZero() {
		session.SetCreatedAt(timestamppb.New(createdAt))
	}
	info := &controltowerv1.ScanSessionInfo{}
	info.SetScanSession(session)
	return info
}

func scanSessionProject(
	projectID string,
	name string,
	version string,
) *controltowerv1.ScanSessionInfo_ProjectWithAttributes {
	project := &messagescontroltowerv1.Project{}
	project.SetProjectId(projectID)
	project.SetName(name)
	projectVersion := &messagescontroltowerv1.ProjectVersion{}
	projectVersion.SetVersion(version)
	attributes := &controltowerv1.ScanSessionInfo_ProjectWithAttributes{}
	attributes.SetProject(project)
	attributes.SetProjectVersion(projectVersion)
	return attributes
}
