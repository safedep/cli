package policy

import (
	"context"
	"strings"
	"testing"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func badRequest(field, description string) error {
	return usefulerror.NewStatus(codes.InvalidArgument, description).
		WithDetail(&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: field, Description: description},
		}}).Err()
}

func TestServerErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantHuman string
		wantHelp  string
	}{
		{
			name:      "a revision above head",
			err:       mapRestoreError(99, badRequest("restore.from_revision", "the revision to restore does not exist")),
			wantCode:  usefulerror.ErrNotFound,
			wantHuman: "Policy revision 99 does not exist",
			wantHelp:  "safedep policy revision list",
		},
		{
			name: "a publish that removes an override with rules",
			err: mapChangeError("publish", "3", status.Error(codes.FailedPrecondition,
				"the change removes a policy override that active package rules attach to: r_1\x1b[2J")),
			wantCode:  usefulerror.ErrConflict,
			wantHuman: `The server refused the request: the change removes a policy override that active package rules attach to: r_1\x1b[2J`,
			wantHelp:  "safedep policy rule list --override NAME",
		},
		{
			name:      "a rule that is already deleted",
			err:       mapRuleError("delete", "r_1", status.Error(codes.FailedPrecondition, "package rule r_1 does not exist at head")),
			wantCode:  usefulerror.ErrConflict,
			wantHuman: "The server refused the request: package rule r_1 does not exist at head",
			wantHelp:  "safedep policy rule list",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ue, ok := usefulerror.AsUsefulError(tt.err)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, ue.Code())
			assert.Equal(t, tt.wantHuman, ue.HumanError())
			assert.Contains(t, ue.Help(), tt.wantHelp)
		})
	}
}

func TestValidateChangeCreate_RevisionZero(t *testing.T) {
	err := validateChangeCreate(changeCreateInput{FromRevisionSet: true, PackageRules: "head"})
	assert.ErrorContains(t, err, "revision 0 has no policy. Use 1 or more")
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"a field of the document", badRequest("settings.overrides[0].name", "value is required"), true},
		{"an override of the document", badRequest("overrides[1].priority", "must be unique"), true},
		{"the title", badRequest("title", "must be at most 200 characters"), false},
		{"the title with no detail", status.Error(codes.InvalidArgument, "title: must be at most 200 characters"), false},
		{"a message with no field", status.Error(codes.InvalidArgument, "override priorities must be unique"), true},
		{"another code", status.Error(codes.Unavailable, "down"), false},
		{
			"an encoded field path of the document",
			badRequest(encodedFieldPath("settings", "base", "package_admission", "cooldown", "min_age"), "must be at most 365d"),
			true,
		},
		{"the title after the service prefix", status.Error(codes.InvalidArgument, "service execution failed: title: too long"), false},
		{
			"the title after a doubled service prefix",
			status.Error(codes.InvalidArgument, "service execution failed: service execution failed: title: too long"),
			false,
		},
		{
			"a mapped error of the document",
			mapChangeError("create", "", badRequest(encodedFieldPath("settings", "overrides[0]", "name"), "does not match")),
			true,
		},
		{"a mapped error of the title", mapChangeError("create", "", status.Error(codes.InvalidArgument, "title: too long")), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isRetryable(tt.err))
		})
	}
}

func TestRunEdit_RefusesALongTitleBeforeTheEditor(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	_, err := runEdit(context.Background(), client, (&scriptedEditor{}).open, "", editInput{Title: strings.Repeat("t", 201)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-m is 201 characters long: the limit is 200")
}

func TestRunEdit_WarnsAboutALabelThatAToolSets(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	createdSettings(client, new(*policyv1.Policy))

	ed := &scriptedEditor{edits: []func(string) string{replace("{custom/environment: ci}", "{reported/os: linux}")}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.NoError(t, err)
	assert.Contains(t, stderr.String(),
		`Policy override "ci" selects on reported/os. A tool sets this label, so a person on the endpoint can change it.`)
	assert.NotContains(t, stderr.String(), `"payments" selects`, "an override with no change gets no warning")
}

func TestSettableSelectorKeys(t *testing.T) {
	selector := &messagescontroltowerv1.LabelSelector{}
	selector.SetMatchLabels(map[string]string{"custom/team": "pay", "managed/kind": "ci"})
	expr := &messagescontroltowerv1.LabelSelectorRequirement{}
	expr.SetKey("scm-property/criticality")
	selector.SetMatchExpressions([]*messagescontroltowerv1.LabelSelectorRequirement{expr})
	assert.Equal(t, []string{"managed/kind", "scm-property/criticality"}, settableSelectorKeys(selector))
}

func TestReportLaggingEndpoints(t *testing.T) {
	tests := []struct {
		revision uint64
		want     bool
	}{
		{1, false},
		{2, true},
	}
	for _, tt := range tests {
		stderr := captureStderr(t)
		reportLaggingEndpoints(tt.revision)
		assert.Equal(t, tt.want, strings.Contains(stderr.String(), "--applied-revision"), "revision %d", tt.revision)
	}
}

func TestDecidedBy_AFixedRuleOfTheCheck(t *testing.T) {
	d := decision("d_fixed", policyv1.DecisionTool_DECISION_TOOL_PULL_REQUEST_CHECK,
		policyv1.DecisionOutcome_DECISION_OUTCOME_FAILED, "evil", "1.0.0",
		citation(47, 1, "", "", "", recordv1.Layer_LAYER_UNSPECIFIED),
		atPullRequest("acme/api", 7, ""))
	assert.Equal(t, "a fixed rule of the check", newDecisionView(d, nil).decidedBy())
}
