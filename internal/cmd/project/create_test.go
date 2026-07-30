package project

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeCreateProjectScansClient struct {
	calls int
	req   *controltowerv1.CreateProjectScansRequest
	res   *controltowerv1.CreateProjectScansResponse
	err   error
}

func (f *fakeCreateProjectScansClient) CreateProjectScans(
	_ context.Context,
	req *controltowerv1.CreateProjectScansRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.CreateProjectScansResponse, error) {
	f.calls++
	f.req = req
	return f.res, f.err
}

var (
	_ createProjectScansClient = (*fakeCreateProjectScansClient)(nil)
	_ createProjectScansClient = controltowerv1grpc.ScanServiceClient(nil)
)

func TestValidateProjectIDs(t *testing.T) {
	t.Parallel()

	oneHundred := make([]string, maxProjectScans)
	for i := range oneHundred {
		oneHundred[i] = string(rune('a' + i))
	}
	oneHundredOne := append(append([]string{}, oneHundred...), "overflow")

	tests := []struct {
		name    string
		ids     []string
		wantErr string
	}{
		{name: "one", ids: []string{"project-1"}},
		{name: "one hundred", ids: oneHundred},
		{name: "zero", wantErr: "between 1 and 100"},
		{name: "one hundred one", ids: oneHundredOne, wantErr: "between 1 and 100"},
		{name: "empty", ids: []string{"project-1", ""}, wantErr: "must not be empty"},
		{name: "duplicate", ids: []string{"project-1", "project-1"}, wantErr: "duplicate project ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateProjectIDs(tt.ids)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCreateProjectScans_SendsOneOrderedBatch(t *testing.T) {
	t.Parallel()

	client := &fakeCreateProjectScansClient{
		res: createResponse(
			projectScan("project-2", "session-2", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{}),
			projectScan("project-1", "session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_RUNNING, time.Time{}),
		),
	}

	result, err := createProjectScans(context.Background(), client, []string{"project-1", "project-2"})
	require.NoError(t, err)

	assert.Equal(t, 1, client.calls)
	require.Len(t, client.req.GetTargets(), 2)
	assert.Equal(t, "project-1", client.req.GetTargets()[0].GetProjectId())
	assert.Equal(t, "project-2", client.req.GetTargets()[1].GetProjectId())

	require.Len(t, result.scans, 2)
	assert.Equal(t, "project-2", result.scans[0].ProjectID)
	assert.Equal(t, "session-2", result.scans[0].ScanSessionID)
	assert.Equal(t, "queued", result.scans[0].Status)
	assert.Equal(t, "project-1", result.scans[1].ProjectID)
	assert.Equal(t, "running", result.scans[1].Status)
}

func TestCreateProjectScans_ConvertsCreatedAtToUTC(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 15, 30, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	client := &fakeCreateProjectScansClient{
		res: createResponse(
			projectScan("project-1", "session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, createdAt),
			projectScan("project-2", "session-2", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{}),
		),
	}

	result, err := createProjectScans(context.Background(), client, []string{"project-1", "project-2"})
	require.NoError(t, err)
	require.Len(t, result.scans, 2)
	require.NotNil(t, result.scans[0].CreatedAt)
	assert.Equal(t, "2026-07-30T10:00:00Z", result.scans[0].CreatedAt.UTC().Format(time.RFC3339))
	assert.Nil(t, result.scans[1].CreatedAt)
}

func TestCreateProjectScans_RejectsInvalidInputBeforeRPC(t *testing.T) {
	t.Parallel()

	client := &fakeCreateProjectScansClient{}
	_, err := createProjectScans(context.Background(), client, []string{"duplicate", "duplicate"})
	require.Error(t, err)
	assert.Zero(t, client.calls)
}

func TestCreateProjectScans_RejectsMalformedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		res     *controltowerv1.CreateProjectScansResponse
		wantErr string
	}{
		{
			name:    "wrong count",
			res:     createResponse(),
			wantErr: "response count",
		},
		{
			name: "missing project ID",
			res: createResponse(projectScan(
				"", "session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{},
			)),
			wantErr: "missing project ID",
		},
		{
			name: "missing session",
			res: createResponse(func() *controltowerv1.CreateProjectScansResponse_ProjectScan {
				scan := &controltowerv1.CreateProjectScansResponse_ProjectScan{}
				scan.SetProjectId("project-1")
				return scan
			}()),
			wantErr: "missing scan session",
		},
		{
			name: "missing session ID message",
			res: createResponse(func() *controltowerv1.CreateProjectScansResponse_ProjectScan {
				scan := &controltowerv1.CreateProjectScansResponse_ProjectScan{}
				scan.SetProjectId("project-1")
				scan.SetScanSession(&messagescontroltowerv1.ScanSession{})
				return scan
			}()),
			wantErr: "missing scan session ID",
		},
		{
			name: "empty session ID",
			res: createResponse(projectScan(
				"project-1", "", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{},
			)),
			wantErr: "missing scan session ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeCreateProjectScansClient{res: tt.res}
			_, err := createProjectScans(context.Background(), client, []string{"project-1"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "project scan create: invalid response")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCreateProjectScans_PreservesGRPCStatusAndDetails(t *testing.T) {
	t.Parallel()

	detail := &errorv1.ErrorDetail{}
	detail.SetReason(errorv1.ErrorReason_ERROR_REASON_QUOTA_EXCEEDED)
	rpcStatus, err := status.New(codes.ResourceExhausted, "quota exceeded").WithDetails(detail)
	require.NoError(t, err)

	client := &fakeCreateProjectScansClient{err: rpcStatus.Err()}
	_, err = createProjectScans(context.Background(), client, []string{"project-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project scan create")
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))

	gotStatus, ok := status.FromError(err)
	require.True(t, ok)
	require.Len(t, gotStatus.Details(), 1)
	gotDetail, ok := gotStatus.Details()[0].(*errorv1.ErrorDetail)
	require.True(t, ok)
	assert.Equal(t, errorv1.ErrorReason_ERROR_REASON_QUOTA_EXCEEDED, gotDetail.GetReason())
}

func TestCreateResult_RenderJSON(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	result := &createResult{scans: []createdScan{
		{ProjectID: "project-1", ScanSessionID: "session-1", Status: "queued", CreatedAt: &createdAt},
		{ProjectID: "project-2", ScanSessionID: "session-2", Status: "queued"},
	}}

	got, err := result.RenderJSON()
	require.NoError(t, err)

	var parsed struct {
		Scans []map[string]any `json:"scans"`
	}
	require.NoError(t, json.Unmarshal(got, &parsed))
	require.Len(t, parsed.Scans, 2)
	assert.Equal(t, "project-1", parsed.Scans[0]["project_id"])
	assert.Equal(t, "session-1", parsed.Scans[0]["scan_session_id"])
	assert.Equal(t, "queued", parsed.Scans[0]["status"])
	assert.Equal(t, "2026-07-30T10:00:00Z", parsed.Scans[0]["created_at"])
	assert.NotContains(t, parsed.Scans[1], "created_at")
}

func TestCreateResult_RenderPlain(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	result := &createResult{scans: []createdScan{
		{ProjectID: "project-1", ScanSessionID: "session-1", Status: "queued", CreatedAt: &createdAt},
	}}

	lines := strings.Split(result.RenderPlain(), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, "project_id\tscan_session_id\tstatus\tcreated_at", lines[0])
	assert.Equal(t, "project-1\tsession-1\tqueued\t2026-07-30T10:00:00Z", lines[1])
}

func TestCreateResult_RenderTable(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	result := &createResult{scans: []createdScan{
		{ProjectID: "project-1", ScanSessionID: "session-1", Status: "queued", CreatedAt: &createdAt},
	}}

	got := result.RenderTable()
	assert.Contains(t, got, "PROJECT ID")
	assert.Contains(t, got, "SCAN SESSION ID")
	assert.Contains(t, got, "STATUS")
	assert.Contains(t, got, "CREATED AT")
	assert.Contains(t, got, "project-1")
	assert.Contains(t, got, "session-1")
	assert.Contains(t, got, "queued")
	assert.Contains(t, got, "2026-07-30T10:00:00Z")
}

func createResponse(
	scans ...*controltowerv1.CreateProjectScansResponse_ProjectScan,
) *controltowerv1.CreateProjectScansResponse {
	res := &controltowerv1.CreateProjectScansResponse{}
	res.SetProjectScans(scans)
	return res
}

func projectScan(
	projectID string,
	sessionID string,
	scanStatus messagescontroltowerv1.ScanStatus,
	createdAt time.Time,
) *controltowerv1.CreateProjectScansResponse_ProjectScan {
	id := &messagescontroltowerv1.ScanSessionId{}
	id.SetSessionId(sessionID)
	session := &messagescontroltowerv1.ScanSession{}
	session.SetScanSessionId(id)
	session.SetStatus(scanStatus)
	if !createdAt.IsZero() {
		session.SetCreatedAt(timestamppb.New(createdAt))
	}
	scan := &controltowerv1.CreateProjectScansResponse_ProjectScan{}
	scan.SetProjectId(projectID)
	scan.SetScanSession(session)
	return scan
}
