package policy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

const prSHA = "0123456789abcdef0123456789abcdef01234567"

func citation(revision, defaultRevision uint64, path, ruleID, overrideID string, layer recordv1.Layer) *recordv1.DecisionCitation {
	c := &recordv1.DecisionCitation{}
	c.SetRevision(revision)
	c.SetDefaultPolicyRevision(defaultRevision)
	c.SetPath(path)
	c.SetRuleId(ruleID)
	c.SetOverrideId(overrideID)
	c.SetLayer(layer)
	return c
}

func endpointPoint(hostname, username string) *messagescontroltowerv1.EndpointPoint {
	e := &messagescontroltowerv1.EndpointPoint{}
	e.SetEndpointId("01JEP0000000000000000000")
	if hostname != "" {
		e.SetHostname(hostname)
	}
	if username != "" {
		e.SetUsername(username)
	}
	e.SetInvocationId("inv_1")
	return e
}

func atEndpoint(e *messagescontroltowerv1.EndpointPoint) *messagescontroltowerv1.DecisionPoint {
	p := &messagescontroltowerv1.DecisionPoint{}
	p.SetEndpoint(e)
	return p
}

func atPullRequest(repo string, number uint32, url string) *messagescontroltowerv1.DecisionPoint {
	pr := &messagescontroltowerv1.PullRequestPoint{}
	pr.SetPlatform(messagescontroltowerv1.IntegrationType_INTEGRATION_TYPE_GITHUB)
	pr.SetRepositoryId("123")
	pr.SetRepositoryName(repo)
	pr.SetNumber(number)
	pr.SetHeadCommitSha(prSHA)
	if url != "" {
		pr.SetUrl(url)
	}
	p := &messagescontroltowerv1.DecisionPoint{}
	p.SetPullRequest(pr)
	return p
}

func atScan(repo, ref string) *messagescontroltowerv1.DecisionPoint {
	s := &messagescontroltowerv1.ScanPoint{}
	s.SetRepositoryName(repo)
	if ref != "" {
		s.SetRef(ref)
	}
	s.SetCommitSha(prSHA)
	s.SetUrl("https://ci.example.com/run/9")
	p := &messagescontroltowerv1.DecisionPoint{}
	p.SetScan(s)
	return p
}

func decision(
	id string,
	tool policyv1.DecisionTool,
	outcome policyv1.DecisionOutcome,
	name, version string,
	c *recordv1.DecisionCitation,
	point *messagescontroltowerv1.DecisionPoint,
) *policyv1.PolicyDecision {
	pkg := &packagev1.Package{}
	pkg.SetEcosystem(packagev1.Ecosystem_ECOSYSTEM_NPM)
	pkg.SetName(name)
	pv := &packagev1.PackageVersion{}
	pv.SetPackage(pkg)
	pv.SetVersion(version)
	d := &policyv1.PolicyDecision{}
	d.SetDecisionId(id)
	d.SetDecidedAt(timestamppb.New(time.Now().Add(-5 * time.Minute)))
	d.SetTool(tool)
	d.SetOutcome(outcome)
	d.SetPackageVersion(pv)
	d.SetCitation(c)
	if point != nil {
		d.SetDecisionPoint(point)
	}
	return d
}

// ruleDecisions are a pmg block and a failed pull request check that one
// package rule made.
func ruleDecisions() []*policyv1.PolicyDecision {
	return []*policyv1.PolicyDecision{
		decision("d_pmg", policyv1.DecisionTool_DECISION_TOOL_PMG, policyv1.DecisionOutcome_DECISION_OUTCOME_BLOCKED,
			"evil", "1.0.0", citation(47, 12, "", "r_9c1x4kq7dm", "", recordv1.Layer_LAYER_UNSPECIFIED),
			atEndpoint(endpointPoint("dev-laptop", "alice"))),
		decision("d_pr", policyv1.DecisionTool_DECISION_TOOL_PULL_REQUEST_CHECK,
			policyv1.DecisionOutcome_DECISION_OUTCOME_FAILED, "evil", "1.0.1",
			citation(47, 12, "", "r_9c1x4kq7dm", "", recordv1.Layer_LAYER_UNSPECIFIED),
			atPullRequest("acme/payments-api", 42, "https://github.com/acme/payments-api/runs/7")),
	}
}

func decisionsResponse(next string, decisions ...*policyv1.PolicyDecision) *policyv1.ListPolicyDecisionsResponse {
	page := &messagescontroltowerv1.PaginationResponse{}
	page.SetNextPageToken(next)
	res := &policyv1.ListPolicyDecisionsResponse{}
	res.SetDecisions(decisions)
	res.SetPagination(page)
	return res
}

func TestNewDecisionFilters(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		name       string
		in         decisionListInput
		overrideID string
		check      func(*testing.T, *policyv1.ListPolicyDecisionsRequest_Filters)
		wantErr    string
	}{
		{
			name:  "no filters",
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) { assert.Nil(t, f) },
		},
		{
			name: "time range",
			in:   decisionListInput{Since: 7 * day, Until: day},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, fixedNow.Add(-7*day), f.GetDecidedAfter().AsTime())
				assert.Equal(t, fixedNow.Add(-day), f.GetDecidedBefore().AsTime())
			},
		},
		{
			name: "tool pmg",
			in:   decisionListInput{Tool: "pmg"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, policyv1.DecisionTool_DECISION_TOOL_PMG, f.GetTool())
			},
		},
		{
			name: "tool pull request check",
			in:   decisionListInput{Tool: "pull-request-check"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, policyv1.DecisionTool_DECISION_TOOL_PULL_REQUEST_CHECK, f.GetTool())
			},
		},
		{
			name: "outcome",
			in:   decisionListInput{Outcome: "allowed"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, policyv1.DecisionOutcome_DECISION_OUTCOME_ALLOWED, f.GetOutcome())
			},
		},
		{
			name: "revision",
			in:   decisionListInput{Revision: 47},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, uint64(47), f.GetRevision())
			},
		},
		{
			name: "rule",
			in:   decisionListInput{Rule: "r_9c1x4kq7dm"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, "r_9c1x4kq7dm", f.GetRuleId())
			},
		},
		{
			name:       "override id from the resolved name",
			in:         decisionListInput{Override: "payments"},
			overrideID: "ov_pay",
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, "ov_pay", f.GetOverrideId())
			},
		},
		{
			name: "path",
			in:   decisionListInput{Path: "package_admission.cooldown.min_age"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, "package_admission.cooldown.min_age", f.GetPath())
			},
		},
		{
			name: "package",
			in:   decisionListInput{Package: "pkg:npm/%40acme/ui"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, packagev1.Ecosystem_ECOSYSTEM_NPM, f.GetPackage().GetEcosystem())
				assert.Equal(t, "@acme/ui", f.GetPackage().GetName())
			},
		},
		{
			name: "project",
			in:   decisionListInput{Project: "prj_1"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, "prj_1", f.GetProjectId())
			},
		},
		{
			name: "endpoint",
			in:   decisionListInput{Endpoint: "01JEP0000000000000000000"},
			check: func(t *testing.T, f *policyv1.ListPolicyDecisionsRequest_Filters) {
				assert.Equal(t, "01JEP0000000000000000000", f.GetEndpointId())
			},
		},
		{name: "unknown tool", in: decisionListInput{Tool: "npm"}, wantErr: "unknown --tool value"},
		{name: "unknown outcome", in: decisionListInput{Outcome: "denied"}, wantErr: "unknown --outcome value"},
		{name: "package with a version", in: decisionListInput{Package: "pkg:npm/lodash@4.17.21"}, wantErr: "has a version"},
		{name: "package not a PURL", in: decisionListInput{Package: "lodash"}, wantErr: "is not a PURL"},
		{name: "package of an unknown ecosystem", in: decisionListInput{Package: "pkg:nope/x"}, wantErr: "unknown ecosystem"},
		{name: "until after since", in: decisionListInput{Since: time.Hour, Until: 2 * time.Hour}, wantErr: "--until must be shorter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := newDecisionFilters(tt.in, tt.overrideID, fixedNow)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				ue, ok := usefulerror.AsUsefulError(err)
				require.True(t, ok)
				assert.Equal(t, usefulerror.ErrBadRequest, ue.Code())
				return
			}
			require.NoError(t, err)
			tt.check(t, f)
		})
	}
}

func TestRunDecisionList_RuleListsBothToolsWithThePlace(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPolicyDecisionsRequest) bool {
		return r.GetFilters().GetRuleId() == "r_9c1x4kq7dm"
	})).Return(decisionsResponse("", ruleDecisions()...), nil)

	got, err := runDecisionList(context.Background(), client, decisionListInput{Rule: "r_9c1x4kq7dm"}, fixedNow)
	require.NoError(t, err)

	table := got.RenderTable()
	for _, want := range []string{
		"5m ago", "pmg", "npm/evil@1.0.0", "blocked", "rule r_9c1x4kq7dm", "dev-laptop (alice)",
		"pull-request-check", "failed", "acme/payments-api#42", "https://github.com/acme/payments-api/runs/7",
		"LINK", "2 decisions",
	} {
		assert.Contains(t, table, want)
	}
	plain := strings.Split(got.RenderPlain(), "\n")
	require.Len(t, plain, 3)
	assert.Contains(t, plain[1], "\td_pmg\tpmg\tblocked\tnpm\tevil\t1.0.0\t47\t12\t\t\tr_9c1x4kq7dm\t\tendpoint\tdev-laptop (alice)\t")
	assert.Contains(t, plain[2], "\tpull-request\tacme/payments-api#42\thttps://github.com/acme/payments-api/runs/7")
}

func TestRunDecisionList_CooldownShowsThePathAndTheDefaultRevision(t *testing.T) {
	cooldown := decision("d_cool", policyv1.DecisionTool_DECISION_TOOL_PMG, policyv1.DecisionOutcome_DECISION_OUTCOME_BLOCKED,
		"fresh", "0.0.1", citation(47, 12, "package_admission.cooldown.min_age", "", "", recordv1.Layer_LAYER_DEFAULT_POLICY),
		atEndpoint(endpointPoint("dev-laptop", "alice")))
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPolicyDecisionsRequest) bool {
		return r.GetFilters().GetPath() == "package_admission.cooldown.min_age"
	})).Return(decisionsResponse("", cooldown), nil)

	got, err := runDecisionList(context.Background(), client,
		decisionListInput{Path: "package_admission.cooldown.min_age"}, fixedNow)
	require.NoError(t, err)
	table := got.RenderTable()
	assert.Contains(t, table, "package_admission.cooldown.min_age · default policy")
	assert.Contains(t, table, "47 · default 12")
	assert.NotContains(t, table, "LINK", "no decision has a link")
	assert.Contains(t, got.RenderPlain(), "\t47\t12\tdefault-policy\tpackage_admission.cooldown.min_age\t")
}

func TestRunDecisionList_Override(t *testing.T) {
	overrideDecision := decision("d_ov", policyv1.DecisionTool_DECISION_TOOL_PMG, policyv1.DecisionOutcome_DECISION_OUTCOME_BLOCKED,
		"fresh", "0.0.1", citation(47, 12, "package_admission.cooldown.min_age", "", "ov_pay", recordv1.Layer_LAYER_OVERRIDE),
		atEndpoint(endpointPoint("dev-laptop", "alice")))
	tests := []struct {
		name     string
		in       decisionListInput
		expect   func(*mocks.PolicyServiceClientMock)
		wantID   string
		wantCell string
		wantErr  string
	}{
		{
			name: "by name at head",
			in:   decisionListInput{Override: "payments"},
			expect: func(c *mocks.PolicyServiceClientMock) {
				c.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil).Once()
			},
			wantID:   "ov_pay",
			wantCell: "package_admission.cooldown.min_age · override payments",
		},
		{
			name: "by id",
			in:   decisionListInput{Override: "ov_pay"},
			expect: func(c *mocks.PolicyServiceClientMock) {
				c.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil).Once()
			},
			wantID:   "ov_pay",
			wantCell: "override payments",
		},
		{
			name: "by name at the revision",
			in:   decisionListInput{Override: "payments", Revision: 47},
			expect: func(c *mocks.PolicyServiceClientMock) {
				res := &policyv1.GetPolicyRevisionResponse{}
				res.SetPolicy(headPolicy())
				c.EXPECT().GetPolicyRevision(mock.Anything, mock.MatchedBy(func(r *policyv1.GetPolicyRevisionRequest) bool {
					return r.GetRevision() == 47
				})).Return(res, nil).Once()
			},
			wantID:   "ov_pay",
			wantCell: "override payments",
		},
		{
			name: "unknown name",
			in:   decisionListInput{Override: "nope"},
			expect: func(c *mocks.PolicyServiceClientMock) {
				c.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil).Once()
			},
			wantErr: `no policy override is named "nope"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewPolicyServiceClientMock(t)
			tt.expect(client)
			if tt.wantErr == "" {
				client.EXPECT().ListPolicyDecisions(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPolicyDecisionsRequest) bool {
					return r.GetFilters().GetOverrideId() == tt.wantID && r.GetFilters().GetRevision() == tt.in.Revision
				})).Return(decisionsResponse("", overrideDecision), nil)
			}
			got, err := runDecisionList(context.Background(), client, tt.in, fixedNow)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, got.RenderTable(), tt.wantCell)
			assert.Contains(t, got.RenderPlain(), "\tov_pay\t", "plain keeps the id")
		})
	}
}

func TestRunDecisionList_OverrideNameReadFailureKeepsTheID(t *testing.T) {
	d := decision("d_ov", policyv1.DecisionTool_DECISION_TOOL_PMG, policyv1.DecisionOutcome_DECISION_OUTCOME_BLOCKED,
		"evil", "1.0.0", citation(47, 12, "", "r_1", "ov_gone", recordv1.Layer_LAYER_UNSPECIFIED),
		atEndpoint(endpointPoint("dev-laptop", "")))
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.Anything).Return(decisionsResponse("", d), nil)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(nil, status.Error(codes.Unavailable, "down"))

	got, err := runDecisionList(context.Background(), client, decisionListInput{}, fixedNow)
	require.NoError(t, err)
	assert.Contains(t, got.RenderTable(), "rule r_1 · override ov_gone")
}

func TestDescribePlace(t *testing.T) {
	ci := endpointPoint("runner-7", "runner")
	ciContext := &messagescontroltowerv1.EndpointCIContext{}
	ciContext.SetRepository("acme/api")
	ciContext.SetBranch("main")
	ci.SetCi(ciContext)

	pod := endpointPoint("api-7d9f-x2", "")
	k8s := &messagescontroltowerv1.EndpointKubernetesContext{}
	k8s.SetNamespace("payments")
	k8s.SetWorkloadName("api")
	pod.SetKubernetes(k8s)

	laptop := endpointPoint("dev-laptop", "alice")
	agent := &messagescontroltowerv1.EndpointAgentContext{}
	agent.SetAgentName("claude-code")
	laptop.SetAgent(agent)

	tests := []struct {
		name      string
		point     *messagescontroltowerv1.DecisionPoint
		wantKind  string
		wantPlace string
		wantURL   string
	}{
		{"endpoint", atEndpoint(endpointPoint("dev-laptop", "alice")), placeEndpoint, "dev-laptop (alice)", ""},
		{"endpoint with no hostname", atEndpoint(endpointPoint("", "")), placeEndpoint, "01JEP0000000000000000000", ""},
		{"endpoint in CI", atEndpoint(ci), placeEndpoint, "runner-7 (runner) · ci acme/api", ""},
		{"endpoint in Kubernetes", atEndpoint(pod), placeEndpoint, "api-7d9f-x2 · k8s payments/api", ""},
		{"endpoint with an agent", atEndpoint(laptop), placeEndpoint, "dev-laptop (alice) · agent claude-code", ""},
		{
			"pull request", atPullRequest("acme/api", 42, "https://github.com/acme/api/pull/42"),
			placePullRequest, "acme/api#42", "https://github.com/acme/api/pull/42",
		},
		{"pull request with no link", atPullRequest("acme/api", 42, ""), placePullRequest, "acme/api#42", ""},
		{"scan of a branch", atScan("acme/api", "main"), placeScan, "acme/api@main", "https://ci.example.com/run/9"},
		{"scan of a commit", atScan("acme/api", ""), placeScan, "acme/api@0123456789ab", "https://ci.example.com/run/9"},
		{"no place", nil, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, place, url := describePlace(tt.point)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantPlace, place)
			assert.Equal(t, tt.wantURL, url)
		})
	}
}

func TestRunDecisionList_EscapesServerText(t *testing.T) {
	d := decision("d_x", policyv1.DecisionTool_DECISION_TOOL_PMG, policyv1.DecisionOutcome_DECISION_OUTCOME_BLOCKED,
		"evil\x1b]0;owned\x07", "1.0.0", citation(47, 12, "a\nb", "", "", recordv1.Layer_LAYER_BASE),
		atEndpoint(endpointPoint("host\u202e", "mallory\tx")))
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.Anything).Return(decisionsResponse("", d), nil)

	got, err := runDecisionList(context.Background(), client, decisionListInput{}, fixedNow)
	require.NoError(t, err)
	for _, out := range []string{got.RenderTable(), got.RenderPlain()} {
		assert.NotContains(t, out, "\x1b")
		assert.NotContains(t, out, "\u202e")
		assert.Contains(t, out, `evil\x1b]0;owned\x07`)
		assert.Contains(t, out, `a\nb`)
		assert.Contains(t, out, `mallory\tx`)
	}
	assert.Len(t, strings.Split(got.RenderPlain(), "\n"), 2, "a line break in a value adds no row")
}

func TestRunDecisionList_Empty(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.Anything).Return(decisionsResponse(""), nil)

	got, err := runDecisionList(context.Background(), client, decisionListInput{}, fixedNow)
	require.NoError(t, err)
	assert.Contains(t, got.RenderTable(), "No policy decisions match")
	assert.Len(t, strings.Split(got.RenderPlain(), "\n"), 1, "plain writes only the header")
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"decisions": []}`, string(b))
}

func TestRunDecisionList_Paging(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPolicyDecisionsRequest) bool {
		return r.GetPagination().GetPageSize() == 2 && r.GetPagination().GetPageToken() == "tok_1" && !r.HasFilters()
	})).Return(decisionsResponse("tok_2", ruleDecisions()...), nil)

	got, err := runDecisionList(context.Background(), client, decisionListInput{PageSize: 2, PageToken: "tok_1"}, fixedNow)
	require.NoError(t, err)
	assert.Contains(t, got.RenderTable(), "2 decisions. More available: --page-token tok_2")
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"next_page_token": "tok_2"`)
}

func TestRunDecisionList_JSONHasTheRawMessages(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().ListPolicyDecisions(mock.Anything, mock.Anything).Return(decisionsResponse("", ruleDecisions()...), nil)

	got, err := runDecisionList(context.Background(), client, decisionListInput{}, fixedNow)
	require.NoError(t, err)
	b, err := got.RenderJSON()
	require.NoError(t, err)

	var out struct {
		Decisions []map[string]any `json:"decisions"`
	}
	require.NoError(t, json.Unmarshal(b, &out))
	require.Len(t, out.Decisions, 2)
	pmg := out.Decisions[0]
	assert.Equal(t, "d_pmg", pmg["decision_id"])
	assert.Equal(t, "DECISION_TOOL_PMG", pmg["tool"])
	assert.Equal(t, "DECISION_OUTCOME_BLOCKED", pmg["outcome"])
	assert.Equal(t, "r_9c1x4kq7dm", pmg["citation"].(map[string]any)["rule_id"])
	assert.Equal(t, "inv_1", pmg["decision_point"].(map[string]any)["endpoint"].(map[string]any)["invocation_id"])
	pr := out.Decisions[1]["decision_point"].(map[string]any)["pull_request"].(map[string]any)
	assert.Equal(t, prSHA, pr["head_commit_sha"])
	assert.Equal(t, "INTEGRATION_TYPE_GITHUB", pr["platform"])
}

func TestMapDecisionListError(t *testing.T) {
	badToken := usefulerror.NewStatus(codes.InvalidArgument, "the page token is not valid").
		WithDetail(&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "pagination.page_token", Description: "the page token is not valid"},
		}}).Err()
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantHuman string
	}{
		{
			name: "feature not enabled",
			err: usefulerror.NewStatus(codes.PermissionDenied, "no entitlement").
				WithReason(errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE).Err(),
			wantCode:  usefulerror.ErrMissingEntitlements,
			wantHuman: "Policy management is not enabled for this tenant",
		},
		{
			name:      "role cannot read",
			err:       status.Error(codes.PermissionDenied, "denied"),
			wantCode:  usefulerror.ErrAuthorizationFailed,
			wantHuman: "Your role cannot read the policy decision log",
		},
		{
			name:      "bad page token",
			err:       badToken,
			wantCode:  usefulerror.ErrBadRequest,
			wantHuman: "The page token is not valid",
		},
		{
			name:      "bad filter",
			err:       status.Error(codes.InvalidArgument, "decided_after must be before decided_before\x1b[2J"),
			wantCode:  usefulerror.ErrBadRequest,
			wantHuman: `The server refused the filters: decided_after must be before decided_before\x1b[2J`,
		},
		{
			name:      "server without the RPC",
			err:       status.Error(codes.Unimplemented, "unknown method"),
			wantCode:  usefulerror.ErrServiceUnavailable,
			wantHuman: "SafeDep Cloud does not have the policy decision log yet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewPolicyServiceClientMock(t)
			client.EXPECT().ListPolicyDecisions(mock.Anything, mock.Anything).Return(nil, tt.err)
			_, err := runDecisionList(context.Background(), client, decisionListInput{}, fixedNow)
			ue, ok := usefulerror.AsUsefulError(err)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, ue.Code())
			assert.Equal(t, tt.wantHuman, ue.HumanError())
			assert.ErrorIs(t, err, tt.err)
		})
	}

	t.Run("other errors keep the cause", func(t *testing.T) {
		cause := status.Error(codes.Internal, "boom")
		err := mapDecisionListError(cause)
		assert.ErrorIs(t, err, cause)
		assert.Contains(t, err.Error(), "policy decision list")
	})
}
