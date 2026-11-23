package workload

import (
	"context"
	"testing"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/cmd/workload/mocks"
)

func TestRunRuleDelete(t *testing.T) {
	rules := rulesResponse(trustRule("r_web", "web", nil), trustRule("r_org", "org", nil))
	tests := []struct {
		name    string
		ref     string
		wantID  string
		delErr  error
		wantErr *usefulErrorWant
	}{
		{name: "by name", ref: "org", wantID: "r_org"},
		{name: "by id", ref: "r_web", wantID: "r_web"},
		{
			name:   "a rule that the tenant does not have",
			ref:    "gone",
			wantID: "gone",
			delErr: status.Error(codes.NotFound, "workload trust rule not found"),
			wantErr: &usefulErrorWant{
				code:  usefulerror.ErrNotFound,
				human: "Workload trust rule gone does not exist",
				help:  "List the rules: safedep workload rule list",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewWorkloadAdminServiceClientMock(t)
			client.EXPECT().ListWorkloadTrustRules(mock.Anything, mock.Anything).Return(rules, nil)
			client.EXPECT().DeleteWorkloadTrustRule(mock.Anything, mock.MatchedBy(func(r *controltowerv1.DeleteWorkloadTrustRuleRequest) bool {
				return r.GetRuleId() == tt.wantID
			})).Return(&controltowerv1.DeleteWorkloadTrustRuleResponse{}, tt.delErr)

			id, err := runRuleDelete(context.Background(), client, tt.ref)

			if tt.wantErr != nil {
				requireUsefulError(t, err, *tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestRunRuleShow_ByNameWithTheProviderName(t *testing.T) {
	rule := trustRule("r_web", "web", map[string]string{"acme/web": "123456"})
	client := mocks.NewWorkloadAdminServiceClientMock(t)
	client.EXPECT().ListWorkloadTrustRules(mock.Anything, mock.Anything).Return(rulesResponse(rule), nil)
	client.EXPECT().GetWorkloadTrustRule(mock.Anything, mock.MatchedBy(func(r *controltowerv1.GetWorkloadTrustRuleRequest) bool {
		return r.GetRuleId() == "r_web"
	})).Return(controltowerv1.GetWorkloadTrustRuleResponse_builder{Rule: rule}.Build(), nil)
	client.EXPECT().ListWorkloadIdentityProviders(mock.Anything, mock.Anything).
		Return(providersResponse(activeProvider("prov_acme", "acme")), nil)

	res, err := runRuleShow(context.Background(), client, "web")

	require.NoError(t, err)
	assert.Equal(t, "r_web", res.Rule.ID)
	assert.Equal(t, "github-actions:acme", res.Rule.Provider)
}

func TestRunRuleList_ProviderFilter(t *testing.T) {
	client := mocks.NewWorkloadAdminServiceClientMock(t)
	client.EXPECT().ListWorkloadIdentityProviders(mock.Anything, mock.Anything).
		Return(providersResponse(activeProvider("prov_acme", "acme")), nil)
	client.EXPECT().ListWorkloadTrustRules(mock.Anything, mock.MatchedBy(func(r *controltowerv1.ListWorkloadTrustRulesRequest) bool {
		return r.GetFilters().GetProviderId() == "prov_acme"
	})).Return(rulesResponse(trustRule("r_org", "org", nil)), nil)

	res, err := runRuleList(context.Background(), client, ruleListInput{Provider: "github-actions:acme"})

	require.NoError(t, err)
	require.Len(t, res.Rules, 1)
	assert.Equal(t, "github-actions:acme", res.Rules[0].Provider)
}

func TestRuleResult_RenderTable(t *testing.T) {
	names := map[string]string{"prov_acme": "github-actions:acme"}
	unknownOperator := condition("environment", controltowerv1.ClaimConditionOperator(9), "prod")
	tests := []struct {
		name    string
		rule    *controltowerv1.WorkloadTrustRule
		names   map[string]string
		want    []string
		notWant []string
	}{
		{
			name: "a rule with a repository and a ref prefix",
			rule: trustRule("r1", "web", map[string]string{"acme/web": "123456"},
				condition(claimRef, prefix, "refs/heads/release/*")),
			names:   names,
			want:    []string{"github-actions:acme", "acme/web (123456)", "ref matches refs/heads/release/*", "manual: "},
			notWant: []string{"every repository", "repository_id"},
		},
		{
			name:  "an org-wide rule",
			rule:  trustRule("r1", "org", nil, condition(claimEventName, equals, "push", "release")),
			names: names,
			want:  []string{"every repository of acme", "event_name = push, release"},
		},
		{
			name: "a provider that the CLI cannot name",
			rule: trustRule("r1", "org", nil),
			want: []string{"prov_acme", "every repository of the provider account"},
		},
		{
			name:  "an operator that the CLI does not know",
			rule:  trustRule("r1", "prod", nil, unknownOperator),
			names: names,
			want:  []string{"environment: a condition that this CLI cannot show"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := (&ruleResult{Rule: newRuleView(tt.rule, tt.names)}).RenderTable()
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			for _, w := range tt.notWant {
				assert.NotContains(t, out, w)
			}
		})
	}
}
