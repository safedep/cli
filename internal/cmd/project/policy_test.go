package project

import (
	"context"
	"errors"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

// fakeSubjectPolicyClient is a hand fake for the one RPC of
// cloudpolicy.SubjectPolicyClient.
type fakeSubjectPolicyClient struct {
	req *policyv1.GetSubjectPolicyRequest
	err error
}

func (f *fakeSubjectPolicyClient) GetSubjectPolicy(
	_ context.Context,
	req *policyv1.GetSubjectPolicyRequest,
	_ ...grpc.CallOption,
) (*policyv1.GetSubjectPolicyResponse, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	header := &specv1.ResolvedPolicyHeader{}
	header.SetRevision(46)
	intended := &specv1.ResolvedPolicy{}
	intended.SetHeader(header)
	res := &policyv1.GetSubjectPolicyResponse{}
	res.SetIntended(intended)
	return res, nil
}

func labeledProject(id, name string, source messagescontroltowerv1.Project_Source, labels map[string]string) *controltowerv1.ProjectWithAttributes {
	p := &messagescontroltowerv1.Project{}
	p.SetProjectId(id)
	p.SetName(name)
	p.SetSource(source)
	p.SetOriginUrl("https://example.com/" + name)
	p.SetCreatedAt(timestamppb.New(time.Now().Add(-72 * time.Hour)))
	attrs := &controltowerv1.ProjectAttributes{}
	var list []*messagescontroltowerv1.Label
	for k, v := range labels {
		l := &messagescontroltowerv1.Label{}
		l.SetKey(k)
		l.SetValue(v)
		list = append(list, l)
	}
	attrs.SetLabels(list)
	attrs.SetVulnerabilities(3)
	out := &controltowerv1.ProjectWithAttributes{}
	out.SetProject(p)
	out.SetAttributes(attrs)
	return out
}

func projectsResponse(projects ...*controltowerv1.ProjectWithAttributes) *controltowerv1.ListProjectsResponse {
	res := &controltowerv1.ListProjectsResponse{}
	res.SetProjects(projects)
	return res
}

func TestNewProjectListRequest(t *testing.T) {
	req, err := newProjectListRequest(projectListInput{
		Selector: "scm/owner=acme", Sources: []string{"github", "bitbucket"}, PageSize: 10,
	})
	require.NoError(t, err)
	f := req.GetFilterV2()
	assert.Equal(t, map[string]string{"scm/owner": "acme"}, f.GetLabelSelector().GetMatchLabels())
	assert.Equal(t, []messagescontroltowerv1.Project_Source{
		messagescontroltowerv1.Project_SOURCE_GITHUB, messagescontroltowerv1.Project_SOURCE_BITBUCKET,
	}, f.GetSources())
	assert.Equal(t, uint32(10), req.GetPagination().GetPageSize())

	req, err = newProjectListRequest(projectListInput{})
	require.NoError(t, err)
	assert.False(t, req.HasFilterV2())

	_, err = newProjectListRequest(projectListInput{Sources: []string{"svn"}})
	assert.ErrorContains(t, err, `unknown --source value "svn"`)
	_, err = newProjectListRequest(projectListInput{Selector: "criticality=high"})
	assert.ErrorContains(t, err, "is not valid")
}

func TestRunProjectList(t *testing.T) {
	client := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(labeledProject("p_1", "acme/payments-api", messagescontroltowerv1.Project_SOURCE_GITHUB,
			map[string]string{"scm/owner": "acme", "custom/team": "pay"})), nil
	}}
	got, err := runProjectList(context.Background(), client, projectListInput{Selector: "scm/owner=acme"})
	require.NoError(t, err)
	require.Len(t, got.Projects, 1)
	assert.Equal(t, []string{"custom/team=pay", "scm/owner=acme"}, got.Projects[0].Labels)

	table := got.RenderTable()
	assert.Contains(t, table, "acme/payments-api")
	assert.Contains(t, table, "custom/team=pay, scm/owner=acme")
	assert.Contains(t, got.RenderPlain(), "p_1\tacme/payments-api\tgithub\tcustom/team=pay,scm/owner=acme\t3\t-\t")
}

func TestRunShow_ProjectWithPolicy(t *testing.T) {
	client := &fakeListProjectsClient{listFn: func(req *controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		assert.Equal(t, []string{"PAY/settlement"}, req.GetFilterV2().GetProjectNames())
		return projectsResponse(
			labeledProject("p_9", "PAY/settlement", messagescontroltowerv1.Project_SOURCE_BITBUCKET,
				map[string]string{"scm/forge": "bitbucket"}),
			labeledProject("p_x", "PAY/settlement-old", messagescontroltowerv1.Project_SOURCE_BITBUCKET, nil),
		), nil
	}}
	policies := &fakeSubjectPolicyClient{}
	got, err := runShow(context.Background(), client, policies, "PAY/settlement")
	require.NoError(t, err)
	assert.Equal(t, "p_9", policies.req.GetSubject().GetId())
	assert.Equal(t, specv1.SubjectKind_SUBJECT_KIND_PROJECT, policies.req.GetSubject().GetKind())

	table := got.RenderTable()
	assert.Contains(t, table, "Project          PAY/settlement")
	assert.Contains(t, table, "bitbucket · https://example.com/PAY/settlement")
	assert.Contains(t, table, "revision 46 · no override matches · base only")
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"revision": 46`)
}

func TestRunShow_PolicyUnavailableStillShowsTheProject(t *testing.T) {
	client := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(labeledProject("p_9", "x", messagescontroltowerv1.Project_SOURCE_GITHUB, nil)), nil
	}}
	got, err := runShow(context.Background(), client, &fakeSubjectPolicyClient{err: errors.New("unimplemented")}, "x")
	require.NoError(t, err)
	assert.Contains(t, got.RenderTable(), "unknown: the policy service did not answer")
}

func TestFindProject(t *testing.T) {
	empty := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(), nil
	}}
	_, err := findProject(context.Background(), empty, "project show", "missing")
	requireUsefulError(t, err, "not_found", "Project not found", `Check the name with: safedep project list --name "missing"`)

	two := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(
			labeledProject("p_1", "shared", messagescontroltowerv1.Project_SOURCE_GITHUB, nil),
			labeledProject("p_2", "shared", messagescontroltowerv1.Project_SOURCE_BITBUCKET, nil),
		), nil
	}}
	_, err = findProject(context.Background(), two, "project show", "shared")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `project show: project name "shared" is ambiguous`)
}

func TestRunPolicyShow(t *testing.T) {
	policies := &fakeSubjectPolicyClient{}
	got, err := runPolicyShow(context.Background(), nil, policies, policyShowInput{ProjectID: "p_7", Fields: []string{"scan_gate"}})
	require.NoError(t, err)
	assert.Equal(t, "p_7", policies.req.GetSubject().GetId())
	assert.True(t, got.Filtered)

	client := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(labeledProject("p_1", "acme/payments-api", messagescontroltowerv1.Project_SOURCE_GITHUB, nil)), nil
	}}
	_, err = runPolicyShow(context.Background(), client, policies, policyShowInput{Name: "acme/payments-api"})
	require.NoError(t, err)
	assert.Equal(t, "p_1", policies.req.GetSubject().GetId())
}

func TestRunLabelSet(t *testing.T) {
	projects := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(
			labeledProject("p_1", "acme/payments-api", messagescontroltowerv1.Project_SOURCE_GITHUB, nil),
			labeledProject("p_2", "acme/ledger", messagescontroltowerv1.Project_SOURCE_GITHUB, nil),
		), nil
	}}
	labels := mocks.NewLabelServiceClientMock(t)
	labels.EXPECT().SetSubjectLabels(mock.Anything, mock.MatchedBy(func(r *policyv1.SetSubjectLabelsRequest) bool {
		ids := []string{}
		for _, s := range r.GetSubjects() {
			ids = append(ids, s.GetId())
		}
		return assert.ObjectsAreEqual([]string{"p_9", "p_1", "p_2"}, ids) &&
			r.GetLabels()[0].GetKey() == "custom/criticality" && r.GetLabels()[0].GetValue() == "high"
	})).Return(&policyv1.SetSubjectLabelsResponse{}, nil)

	n, err := runLabelSet(context.Background(), projects, labels, labelInput{
		Names: []string{"acme/payments-api", "acme/ledger"}, ProjectIDs: []string{"p_9"},
		Labels: []string{"custom/criticality=high"},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestRunLabelSet_RejectsOtherPrefixesBeforeAnyRPC(t *testing.T) {
	_, err := runLabelSet(context.Background(), nil, mocks.NewLabelServiceClientMock(t), labelInput{
		ProjectIDs: []string{"p_1"}, Labels: []string{"scm/owner=acme"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `an admin writes only "custom/" labels`)
}

func TestRunLabelDelete(t *testing.T) {
	labels := mocks.NewLabelServiceClientMock(t)
	labels.EXPECT().DeleteSubjectLabels(mock.Anything, mock.MatchedBy(func(r *policyv1.DeleteSubjectLabelsRequest) bool {
		return r.GetKeys()[0] == "custom/criticality" && r.GetSubjects()[0].GetId() == "p_1"
	})).Return(&policyv1.DeleteSubjectLabelsResponse{}, nil)
	n, err := runLabelDelete(context.Background(), &fakeListProjectsClient{}, labels, labelInput{
		ProjectIDs: []string{"p_1"}, Keys: []string{"custom/criticality"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestRequireProjects(t *testing.T) {
	assert.ErrorContains(t, requireProjects(labelInput{}), "give at least one project")
	assert.ErrorContains(t, requireProjects(labelInput{Names: []string{"a", "a"}}), `duplicate project name "a"`)
}

func TestRunProjectList_EscapesLabelValues(t *testing.T) {
	client := &fakeListProjectsClient{listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
		return projectsResponse(labeledProject("p_1", "acme/x", messagescontroltowerv1.Project_SOURCE_GITHUB,
			map[string]string{"custom/team": "pay\x1b[31m\u202e"})), nil
	}}
	got, err := runProjectList(context.Background(), client, projectListInput{})
	require.NoError(t, err)
	for _, out := range []string{got.RenderTable(), got.RenderPlain()} {
		assert.NotContains(t, out, "\x1b")
		assert.Contains(t, out, `custom/team=pay\x1b[31m\u202e`)
	}
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `\u001b[31m`, "JSON keeps the raw value")
}
