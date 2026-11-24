package workload

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/output"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/cmd/workload/mocks"
)

const githubActions = controltowerv1.WorkloadIdentityProviderKind_WORKLOAD_IDENTITY_PROVIDER_KIND_GITHUB_ACTIONS

func githubProvider(id, login string, state controltowerv1.WorkloadIdentityProviderLinkState) *controltowerv1.WorkloadIdentityProvider {
	return controltowerv1.WorkloadIdentityProvider_builder{
		ProviderId:   id,
		Kind:         githubActions,
		OwnerId:      "1001",
		AccountLogin: login,
		LinkState:    state,
		LinkId:       "link-" + id,
	}.Build()
}

func activeProvider(id, login string) *controltowerv1.WorkloadIdentityProvider {
	return githubProvider(id, login, controltowerv1.WorkloadIdentityProviderLinkState_WORKLOAD_IDENTITY_PROVIDER_LINK_STATE_ACTIVE)
}

func providersResponse(providers ...*controltowerv1.WorkloadIdentityProvider) *controltowerv1.ListWorkloadIdentityProvidersResponse {
	return controltowerv1.ListWorkloadIdentityProvidersResponse_builder{Providers: providers}.Build()
}

func condition(claim string, operator controltowerv1.ClaimConditionOperator, values ...string) *controltowerv1.WorkloadClaimCondition {
	return controltowerv1.WorkloadClaimCondition_builder{Claim: claim, Operator: operator, Values: values}.Build()
}

const (
	equals = controltowerv1.ClaimConditionOperator_CLAIM_CONDITION_OPERATOR_EQUALS
	prefix = controltowerv1.ClaimConditionOperator_CLAIM_CONDITION_OPERATOR_PREFIX
)

// trustRule is a rule of the provider prov_acme. repositories maps full
// names to ids. A rule with repositories has the repository_id condition
// that the server makes from them.
func trustRule(id, name string, repositories map[string]string, conditions ...*controltowerv1.WorkloadClaimCondition) *controltowerv1.WorkloadTrustRule {
	var repos []*controltowerv1.WorkloadRepository
	var ids []string
	for fullName, repoID := range repositories {
		repos = append(repos, controltowerv1.WorkloadRepository_builder{RepositoryId: repoID, FullName: fullName}.Build())
		ids = append(ids, repoID)
	}
	if len(ids) > 0 {
		conditions = append(conditions, condition(claimRepositoryID, equals, ids...))
	}
	return controltowerv1.WorkloadTrustRule_builder{
		RuleId:         id,
		Name:           name,
		ProviderId:     "prov_acme",
		Grant:          messagescontroltowerv1.CredentialGrant_CREDENTIAL_GRANT_TOOL,
		Conditions:     conditions,
		Repositories:   repos,
		ProjectBinding: controltowerv1.WorkloadProjectBinding_WORKLOAD_PROJECT_BINDING_MANUAL,
	}.Build()
}

func rulesResponse(rules ...*controltowerv1.WorkloadTrustRule) *controltowerv1.ListWorkloadTrustRulesResponse {
	return controltowerv1.ListWorkloadTrustRulesResponse_builder{Rules: rules}.Build()
}

type usefulErrorWant struct {
	code  string
	human string
	help  string
}

func requireUsefulError(t require.TestingT, err error, want usefulErrorWant) {
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok, "want a useful error, got %v", err)
	require.Equal(t, want.code, ue.Code())
	require.Equal(t, want.human, ue.HumanError())
	require.Equal(t, want.help, ue.Help())
}

// captureStderr sends the messages of dry/tui to a buffer for one test.
func captureStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	output.SetWriters(os.Stdout, &buf)
	t.Cleanup(func() { output.SetWriters(os.Stdout, os.Stderr) })
	return &buf
}

// pageTokens gives the page token that asks for page i and the next page
// token that page i returns. The first page has no token and the last page
// has no next page token.
func pageTokens(i, pages int) (token, next string) {
	if i > 0 {
		token = fmt.Sprintf("p%d", i)
	}
	if i < pages-1 {
		next = fmt.Sprintf("p%d", i+1)
	}
	return token, next
}

// expectRulePages serves the rules in pages. A command that finds what it
// needs on an early page may not read the later ones.
func expectRulePages(client *mocks.WorkloadAdminServiceClientMock, pages ...[]*controltowerv1.WorkloadTrustRule) {
	for i, page := range pages {
		token, next := pageTokens(i, len(pages))
		client.EXPECT().ListWorkloadTrustRules(mock.Anything, mock.MatchedBy(func(r *controltowerv1.ListWorkloadTrustRulesRequest) bool {
			return r.GetPagination().GetPageToken() == token
		})).Return(controltowerv1.ListWorkloadTrustRulesResponse_builder{
			Rules:      page,
			Pagination: messagescontroltowerv1.PaginationResponse_builder{NextPageToken: next}.Build(),
		}.Build(), nil).Maybe()
	}
}

// expectProviderPages serves the providers in pages.
func expectProviderPages(client *mocks.WorkloadAdminServiceClientMock, pages ...[]*controltowerv1.WorkloadIdentityProvider) {
	for i, page := range pages {
		token, next := pageTokens(i, len(pages))
		client.EXPECT().ListWorkloadIdentityProviders(mock.Anything, mock.MatchedBy(func(r *controltowerv1.ListWorkloadIdentityProvidersRequest) bool {
			return r.GetPagination().GetPageToken() == token
		})).Return(controltowerv1.ListWorkloadIdentityProvidersResponse_builder{
			Providers:  page,
			Pagination: messagescontroltowerv1.PaginationResponse_builder{NextPageToken: next}.Build(),
		}.Build(), nil).Once()
	}
}
