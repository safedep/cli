package endpoint

import (
	"context"
	"testing"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

const testEndpointID = "01KR0EKN6PMW0ZRFRN992H1PKX"

// listEndpointsClient fakes the one RPC that Service.List calls. The
// embedded interface is nil, so any other RPC panics the test.
type listEndpointsClient struct {
	controltowerv1grpc.EndpointManagementServiceClient
	req *controltowerv1.ListEndpointsRequest
	res *controltowerv1.ListEndpointsResponse
}

func (c *listEndpointsClient) ListEndpoints(
	_ context.Context,
	req *controltowerv1.ListEndpointsRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListEndpointsResponse, error) {
	c.req = req
	return c.res, nil
}

func toolPolicy(tool string, revision uint64) *controltowerv1.EndpointToolPolicy {
	report := &recordv1.ApplyReport{}
	report.SetRevision(revision)
	report.SetState(recordv1.ApplyState_APPLY_STATE_STALE)
	p := &controltowerv1.EndpointToolPolicy{}
	p.SetToolName(tool)
	p.SetToolVersion("2.3.1")
	p.SetPolicy(report)
	p.SetReportedAt(timestamppb.New(time.Now()))
	return p
}

func TestServiceList_SendsThePolicyFilters(t *testing.T) {
	ep := &controltowerv1.ListEndpointsResponse_EndpointWithAttributes{}
	ep.SetEndpointId(testEndpointID)
	ep.SetToolPolicies([]*controltowerv1.EndpointToolPolicy{toolPolicy("pmg", 47), toolPolicy("vet", 46)})
	res := &controltowerv1.ListEndpointsResponse{}
	res.SetEndpoints([]*controltowerv1.ListEndpointsResponse_EndpointWithAttributes{ep})
	fake := &listEndpointsClient{res: res}

	selector, applied, err := parsePolicyFilters("reported/os=darwin", "<48")
	require.NoError(t, err)
	out, err := (&Service{client: fake}).List(context.Background(), ListInput{LabelSelector: selector, AppliedRevisionBelow: applied})
	require.NoError(t, err)

	f := fake.req.GetFilter()
	assert.Equal(t, uint64(48), f.GetAppliedPolicyRevisionBelow())
	assert.Equal(t, map[string]string{"reported/os": "darwin"}, f.GetLabelSelector().GetMatchLabels())
	require.Len(t, out.Endpoints, 1)
	assert.Equal(t, []ToolPolicy{
		{Tool: "pmg", Version: "2.3.1", Revision: 47, State: "stale", ReportedAt: out.Endpoints[0].ToolPolicies[0].ReportedAt},
		{Tool: "vet", Version: "2.3.1", Revision: 46, State: "stale", ReportedAt: out.Endpoints[0].ToolPolicies[1].ReportedAt},
	}, out.Endpoints[0].ToolPolicies)
	assert.Equal(t, "pmg 47, vet 46", policyCell(out.Endpoints[0].ToolPolicies))
}

func TestParsePolicyFilters(t *testing.T) {
	tests := []struct {
		applied string
		want    uint64
		wantErr bool
	}{
		{applied: "", want: 0},
		{applied: "<48", want: 48},
		{applied: " < 48 ", want: 48},
		{applied: "48", wantErr: true},
		{applied: ">48", wantErr: true},
		{applied: "<1", wantErr: true},
		{applied: "<x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.applied, func(t *testing.T) {
			_, got, err := parsePolicyFilters("", tt.applied)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "write '<N'")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	_, _, err := parsePolicyFilters("deployment-kind=ci", "")
	assert.ErrorContains(t, err, "is not valid")
}

func TestPolicyCell(t *testing.T) {
	tests := []struct {
		name     string
		policies []ToolPolicy
		want     string
	}{
		{"no tool", nil, "-"},
		{"one revision", []ToolPolicy{{Tool: "pmg", Revision: 47}, {Tool: "vet", Revision: 47}}, "rev 47"},
		{"two revisions", []ToolPolicy{{Tool: "pmg", Revision: 47}, {Tool: "vet", Revision: 46}}, "pmg 47, vet 46"},
		{"only a disabled tool", []ToolPolicy{{Tool: "pmg", Revision: 0, State: "disabled"}}, "-"},
		{
			"a disabled tool and an applied tool",
			[]ToolPolicy{{Tool: "pmg", Revision: 0, State: "disabled"}, {Tool: "vet", Revision: 46, State: "applied"}},
			"rev 46",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policyCell(tt.policies))
		})
	}
}

func TestList_DisabledToolIsNotInThePolicyCell(t *testing.T) {
	report := &recordv1.ApplyReport{}
	report.SetState(recordv1.ApplyState_APPLY_STATE_DISABLED)
	p := &controltowerv1.EndpointToolPolicy{}
	p.SetToolName("pmg")
	p.SetPolicy(report)
	assert.Equal(t, "-", policyCell(toolPolicies([]*controltowerv1.EndpointToolPolicy{p})))
}

func TestRunList_ShowsTheAppliedRevision(t *testing.T) {
	f := &fakeLister{out: &ListResult{Endpoints: []ListEndpoint{{
		ID: testEndpointID, Hostname: "arjuns-mbp", ToolPolicies: []ToolPolicy{{Tool: "pmg", Revision: 47, State: "applied"}},
	}}}}
	res, err := runList(context.Background(), f, newNopDirectory(t), listInput{AppliedRevisionBelow: 48})
	require.NoError(t, err)
	assert.Equal(t, uint64(48), f.in.AppliedRevisionBelow)
	assert.Contains(t, res.RenderTable(), "rev 47")
	b, err := res.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"applied_policy_revision": 47`)
}

func TestRunLabelSet_ResolvesCachedHostnames(t *testing.T) {
	dir := newTestDirectory(t)
	require.NoError(t, dir.Upsert(context.Background(), []DirectoryEntry{{ID: testEndpointID, Hostname: "arjuns-mbp"}}))
	labels, err := cloudpolicy.ParseCustomLabels([]string{"custom/team=release"})
	require.NoError(t, err)

	client := mocks.NewLabelServiceClientMock(t)
	client.EXPECT().SetSubjectLabels(mock.Anything, mock.MatchedBy(func(r *policyv1.SetSubjectLabelsRequest) bool {
		s := r.GetSubjects()[0]
		return s.GetKind() == specv1.SubjectKind_SUBJECT_KIND_ENDPOINT && s.GetId() == testEndpointID
	})).Return(&policyv1.SetSubjectLabelsResponse{}, nil)

	n, err := runLabelSet(context.Background(), client, dir, []string{"arjuns-mbp"}, labels)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	_, err = runLabelSet(context.Background(), client, dir, []string{"unknown-host"}, labels)
	assert.ErrorContains(t, err, `endpoint "unknown-host"`)
}

func TestRunLabelDelete(t *testing.T) {
	client := mocks.NewLabelServiceClientMock(t)
	client.EXPECT().DeleteSubjectLabels(mock.Anything, mock.Anything).Return(&policyv1.DeleteSubjectLabelsResponse{}, nil)
	n, err := runLabelDelete(context.Background(), client, newTestDirectory(t), []string{testEndpointID}, []string{"custom/team"})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestRunPolicyShow(t *testing.T) {
	dir := newTestDirectory(t)
	require.NoError(t, dir.Upsert(context.Background(), []DirectoryEntry{{ID: testEndpointID, Hostname: "arjuns-mbp"}}))

	client := mocks.NewPolicyServiceClientMock(t)
	header := &specv1.ResolvedPolicyHeader{}
	header.SetRevision(52)
	intended := &specv1.ResolvedPolicy{}
	intended.SetHeader(header)
	res := &policyv1.GetSubjectPolicyResponse{}
	res.SetIntended(intended)
	client.EXPECT().GetSubjectPolicy(mock.Anything, mock.MatchedBy(func(r *policyv1.GetSubjectPolicyRequest) bool {
		return r.GetSubject().GetKind() == specv1.SubjectKind_SUBJECT_KIND_ENDPOINT && r.GetSubject().GetId() == testEndpointID
	})).Return(res, nil)

	got, err := runPolicyShow(context.Background(), client, dir, "arjuns-mbp", nil)
	require.NoError(t, err)
	assert.Equal(t, "arjuns-mbp", got.Name)
	assert.Contains(t, got.RenderTable(), "Endpoint")
	assert.Contains(t, got.RenderTable(), "revision 52")
}
