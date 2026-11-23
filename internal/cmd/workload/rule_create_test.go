package workload

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/cmd/workload/mocks"
)

func TestRunRuleCreate_Provider(t *testing.T) {
	acme := activeProvider("prov_acme", "acme")
	beta := githubProvider("prov_beta", "Beta",
		controltowerv1.WorkloadIdentityProviderLinkState_WORKLOAD_IDENTITY_PROVIDER_LINK_STATE_SUSPENDED)
	listed := "The providers of the tenant:\n" +
		"  github-actions:acme  id prov_acme, link active\n" +
		"  github-actions:Beta  id prov_beta, link suspended"
	tests := []struct {
		name      string
		providers []*controltowerv1.WorkloadIdentityProvider
		provider  string
		wantID    string
		wantErr   usefulErrorWant
	}{
		{
			name:      "no provider with one provider in the tenant",
			providers: []*controltowerv1.WorkloadIdentityProvider{acme},
			wantErr: usefulErrorWant{
				code: usefulerror.ErrBadRequest,
				human: "--provider is required. The CLI never picks a provider. The providers of the tenant:\n" +
					"  github-actions:acme  id prov_acme, link active",
				help: "Run the command again with --provider github-actions:acme",
			},
		},
		{
			name:      "no provider with two providers",
			providers: []*controltowerv1.WorkloadIdentityProvider{acme, beta},
			wantErr: usefulErrorWant{
				code:  usefulerror.ErrBadRequest,
				human: "--provider is required. The CLI never picks a provider. " + listed,
				help:  "Run the command again with --provider github-actions:acme",
			},
		},
		{
			name: "no provider in a tenant with none",
			wantErr: usefulErrorWant{
				code:  usefulerror.ErrBadRequest,
				human: "--provider is required. The CLI never picks a provider. The tenant has no workload identity provider",
				help:  noProvidersHelp,
			},
		},
		{
			name:      "a provider that the tenant does not have",
			providers: []*controltowerv1.WorkloadIdentityProvider{acme, beta},
			provider:  "github-actions:gamma",
			wantErr: usefulErrorWant{
				code:  usefulerror.ErrBadRequest,
				human: "--provider github-actions:gamma is not a provider of the tenant. " + listed,
				help:  "Run the command again with --provider github-actions:acme",
			},
		},
		{
			name:      "KIND:ACCOUNT with the account in another case",
			providers: []*controltowerv1.WorkloadIdentityProvider{acme, beta},
			provider:  "github-actions:beta",
			wantID:    "prov_beta",
		},
		{
			name:      "a provider id",
			providers: []*controltowerv1.WorkloadIdentityProvider{acme, beta},
			provider:  "prov_acme",
			wantID:    "prov_acme",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewWorkloadAdminServiceClientMock(t)
			client.EXPECT().ListWorkloadIdentityProviders(mock.Anything, mock.Anything).Return(providersResponse(tt.providers...), nil)
			if tt.wantID != "" {
				client.EXPECT().CreateWorkloadTrustRule(mock.Anything, mock.MatchedBy(func(r *controltowerv1.CreateWorkloadTrustRuleRequest) bool {
					return r.GetProviderId() == tt.wantID
				})).Return(controltowerv1.CreateWorkloadTrustRuleResponse_builder{Rule: trustRule("r1", "org", nil)}.Build(), nil)
			}

			_, err := runRuleCreate(context.Background(), client, ruleCreateInput{Name: "org", Provider: tt.provider, Grant: "tool"})

			if tt.wantID != "" {
				require.NoError(t, err)
				return
			}
			requireUsefulError(t, err, tt.wantErr)
		})
	}
}

func TestRunRuleCreate_ReturnsTheRepositoryID(t *testing.T) {
	client := mocks.NewWorkloadAdminServiceClientMock(t)
	client.EXPECT().ListWorkloadIdentityProviders(mock.Anything, mock.Anything).
		Return(providersResponse(activeProvider("prov_acme", "acme")), nil)
	var sent *controltowerv1.CreateWorkloadTrustRuleRequest
	client.EXPECT().CreateWorkloadTrustRule(mock.Anything, mock.Anything).
		Run(func(_ context.Context, r *controltowerv1.CreateWorkloadTrustRuleRequest, _ ...grpc.CallOption) {
			sent = r
		}).
		Return(controltowerv1.CreateWorkloadTrustRuleResponse_builder{
			Rule: trustRule("r1", "web", map[string]string{"acme/web": "123456"},
				condition(claimRef, equals, "refs/heads/main"), condition(claimEventName, equals, "push")),
		}.Build(), nil)

	res, err := runRuleCreate(context.Background(), client, ruleCreateInput{
		Name: "web", Provider: "github-actions:acme", Grant: "tool",
		Repositories: []string{"acme/web", "acme/web"}, Refs: []string{"refs/heads/main"}, Events: []string{"push"},
	})
	require.NoError(t, err)

	require.NotNil(t, sent)
	assert.Equal(t, "web", sent.GetName())
	assert.Equal(t, "prov_acme", sent.GetProviderId())
	assert.Equal(t, messagescontroltowerv1.CredentialGrant_CREDENTIAL_GRANT_TOOL, sent.GetGrant())
	assert.Equal(t, []string{"acme/web"}, sent.GetRepositories())
	require.Len(t, sent.GetConditions(), 2)
	assert.Equal(t, claimRef, sent.GetConditions()[0].GetClaim())
	assert.Equal(t, equals, sent.GetConditions()[0].GetOperator())
	assert.Equal(t, claimEventName, sent.GetConditions()[1].GetClaim())

	assert.Equal(t, []repositoryView{{ID: "123456", FullName: "acme/web"}}, res.Rule.Repositories)
	assert.False(t, res.Rule.EveryRepository)
	assert.Equal(t, "github-actions:acme", res.Rule.Provider)
	out, err := res.RenderJSON()
	require.NoError(t, err)
	var decoded struct {
		Rule struct {
			Repositories []map[string]string `json:"repositories"`
		} `json:"rule"`
	}
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, []map[string]string{{"repository_id": "123456", "full_name": "acme/web"}}, decoded.Rule.Repositories)
}

func TestRunRuleCreate_ServerErrors(t *testing.T) {
	notAccessible := usefulerror.NewStatus(codes.NotFound, "the GitHub installation of the provider cannot see a repository").
		WithReason(errorv1.ErrorReason_ERROR_REASON_GITHUB_REPOSITORY_NOT_ACCESSIBLE).
		WithDetail(&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{{
			Field:       "repositories[1]",
			Description: "other/web is not a repository of the provider account that the installation can see",
		}}}).Err()
	suspended := usefulerror.NewStatus(codes.FailedPrecondition, "the GitHub App installation of the provider is suspended").
		WithReason(errorv1.ErrorReason_ERROR_REASON_GITHUB_INSTALLATION_SUSPENDED).Err()
	badClaim := usefulerror.NewStatus(codes.InvalidArgument, "service execution failed: invalid condition").
		WithDetail(&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{{
			Field: "conditions[1].claim", Description: "the preset does not allow the claim",
		}}}).Err()
	tests := []struct {
		name string
		err  error
		want usefulErrorWant
	}{
		{
			name: "a repository outside the installation",
			err:  notAccessible,
			want: usefulErrorWant{
				code: usefulerror.ErrNotFound,
				human: "the GitHub installation of the provider cannot see a repository\n" +
					"other/web is not a repository of the provider account that the installation can see",
				help: "Name only repositories of github-actions:acme that the SafeDep GitHub App installation can see. " +
					"An owner of the GitHub organization can add a repository to the installation in the GitHub settings of the organization",
			},
		},
		{
			name: "a suspended installation",
			err:  suspended,
			want: usefulErrorWant{
				code:  usefulerror.ErrConflict,
				human: "GitHub suspended the SafeDep GitHub App installation of github-actions:acme",
				help:  "An owner of the GitHub organization can end the suspension in the GitHub settings of the organization. Then run the command again",
			},
		},
		{
			name: "an unlinked provider",
			err:  status.Error(codes.FailedPrecondition, "service execution failed: the GitHub App link of the provider is unlinked"),
			want: usefulErrorWant{
				code:  usefulerror.ErrConflict,
				human: "The server refused the rule: the GitHub App link of the provider is unlinked",
				help: "Link the GitHub organization again with the SafeDep GitHub App in SafeDep Cloud. " +
					"See the link state: safedep workload provider list",
			},
		},
		{
			name: "a name that a rule has",
			err:  status.Error(codes.AlreadyExists, "a workload trust rule with this name exists"),
			want: usefulErrorWant{
				code:  usefulerror.ErrConflict,
				human: "A workload trust rule with the name web exists",
				help:  "See it with: safedep workload rule show web",
			},
		},
		{
			name: "a claim that the preset does not allow",
			err:  badClaim,
			want: usefulErrorWant{
				code:  usefulerror.ErrBadRequest,
				human: "invalid condition\nconditions[1].claim: the preset does not allow the claim",
				help:  "See the flags: safedep workload rule create --help",
			},
		},
		{
			name: "a member",
			err:  status.Error(codes.PermissionDenied, "admin role required"),
			want: usefulErrorWant{
				code:  usefulerror.ErrAuthorizationFailed,
				human: "Only an Admin can manage workload identity",
				help:  "Ask an Admin of the tenant to run the command",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewWorkloadAdminServiceClientMock(t)
			client.EXPECT().ListWorkloadIdentityProviders(mock.Anything, mock.Anything).
				Return(providersResponse(activeProvider("prov_acme", "acme")), nil)
			client.EXPECT().CreateWorkloadTrustRule(mock.Anything, mock.Anything).Return(nil, tt.err)

			_, err := runRuleCreate(context.Background(), client, ruleCreateInput{
				Name: "web", Provider: "github-actions:acme", Grant: "tool", Repositories: []string{"acme/web", "other/web"},
			})

			requireUsefulError(t, err, tt.want)
		})
	}
}

func TestNewRuleCreateRequest(t *testing.T) {
	valid := ruleCreateInput{Name: "web-release", Grant: "tool"}
	tests := []struct {
		name    string
		mutate  func(*ruleCreateInput)
		wantErr string
		check   func(t *testing.T, r *controltowerv1.CreateWorkloadTrustRuleRequest)
	}{
		{name: "an org-wide rule", mutate: func(*ruleCreateInput) {}, check: func(t *testing.T, r *controltowerv1.CreateWorkloadTrustRuleRequest) {
			assert.Empty(t, r.GetRepositories())
			assert.Empty(t, r.GetConditions())
		}},
		{name: "a name in upper case", mutate: func(in *ruleCreateInput) { in.Name = "Web" }, wantErr: `rule name "Web"`},
		{name: "a name that ends with a hyphen", mutate: func(in *ruleCreateInput) { in.Name = "web-" }, wantErr: `rule name "web-"`},
		{name: "no grant", mutate: func(in *ruleCreateInput) { in.Grant = "" }, wantErr: "--grant is required"},
		{name: "an unknown grant", mutate: func(in *ruleCreateInput) { in.Grant = "admin" }, wantErr: `--grant "admin" is not a grant`},
		{name: "a repository with no owner", mutate: func(in *ruleCreateInput) { in.Repositories = []string{"web"} }, wantErr: `--repository "web": write OWNER/NAME`},
		{name: "a repository path", mutate: func(in *ruleCreateInput) { in.Repositories = []string{"acme/web/x"} }, wantErr: "write OWNER/NAME"},
		{name: "a dot segment", mutate: func(in *ruleCreateInput) { in.Repositories = []string{"acme/.."} }, wantErr: "write OWNER/NAME"},
		{name: "a short ref", mutate: func(in *ruleCreateInput) { in.Refs = []string{"main"} }, wantErr: `--ref "main": write the full ref`},
		{name: "a prefix that ends in a name", mutate: func(in *ruleCreateInput) { in.Refs = []string{"refs/heads/main*"} }, wantErr: "a prefix is refs/heads/"},
		{name: "a star inside a ref", mutate: func(in *ruleCreateInput) { in.Refs = []string{"refs/heads/*/x"} }, wantErr: "a prefix is refs/heads/"},
		{name: "full refs and prefixes", mutate: func(in *ruleCreateInput) { in.Refs = []string{"refs/heads/main", "refs/heads/release/*"} }, wantErr: "full refs or prefixes, not both"},
		{name: "an event in upper case", mutate: func(in *ruleCreateInput) { in.Events = []string{"Push"} }, wantErr: `--event "Push"`},
		{name: "prefixes", mutate: func(in *ruleCreateInput) { in.Refs = []string{"refs/heads/release/*", "refs/tags/*"} },
			check: func(t *testing.T, r *controltowerv1.CreateWorkloadTrustRuleRequest) {
				require.Len(t, r.GetConditions(), 1)
				assert.Equal(t, prefix, r.GetConditions()[0].GetOperator())
				assert.Equal(t, []string{"refs/heads/release/*", "refs/tags/*"}, r.GetConditions()[0].GetValues())
			}},
		{name: "repeated events", mutate: func(in *ruleCreateInput) { in.Events = []string{"push", "release", "push"} },
			check: func(t *testing.T, r *controltowerv1.CreateWorkloadTrustRuleRequest) {
				require.Len(t, r.GetConditions(), 1)
				assert.Equal(t, claimEventName, r.GetConditions()[0].GetClaim())
				assert.Equal(t, []string{"push", "release"}, r.GetConditions()[0].GetValues())
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.mutate(&in)
			req, err := newRuleCreateRequest(in)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, req)
		})
	}
}

// serverPrefixPattern is the protovalidate rule of a PREFIX value in
// WorkloadClaimCondition.
var serverPrefixPattern = regexp.MustCompile(`^refs/(heads|tags)/([^*]*/)?[*]$`)

// FuzzRefCondition checks that each condition that the CLI sends meets the
// rules of the server: a PREFIX value has one final "*" after a "/", an
// EQUALS value has no "*", and no value repeats.
func FuzzRefCondition(f *testing.F) {
	for _, seed := range [][2]string{
		{"refs/heads/main", "refs/tags/v1"},
		{"refs/heads/release/*", "refs/tags/*"},
		{"refs/heads/main*", ""},
		{"refs/heads/*", "refs/heads/*"},
		{"main", "refs/heads/a/*/b"},
		{"refs/pull/1/merge", "refs/heads/**"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		refs := []string{a}
		if b != "" {
			refs = append(refs, b)
		}
		c, err := refCondition(refs)
		if err != nil {
			return
		}
		require.NotNil(t, c)
		assert.Equal(t, claimRef, c.GetClaim())
		seen := map[string]bool{}
		for _, v := range c.GetValues() {
			assert.False(t, seen[v], "value %q repeats", v)
			seen[v] = true
			switch c.GetOperator() {
			case prefix:
				assert.Regexp(t, serverPrefixPattern, v)
			case equals:
				assert.False(t, strings.Contains(v, "*"), "EQUALS value %q has a star", v)
				assert.True(t, strings.HasPrefix(v, "refs/"))
			default:
				t.Fatalf("operator %v", c.GetOperator())
			}
		}
	})
}

func TestReportRuleCreate(t *testing.T) {
	tests := []struct {
		name      string
		rule      *controltowerv1.WorkloadTrustRule
		wantCover bool
	}{
		{name: "a rule with no repository", rule: trustRule("r1", "org", nil), wantCover: true},
		{name: "a rule with a ref only", rule: trustRule("r1", "main", nil, condition(claimRef, equals, "refs/heads/main")), wantCover: true},
		{name: "a rule with a repository", rule: trustRule("r1", "web", map[string]string{"acme/web": "123456"}), wantCover: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := captureStderr(t)
			reportRuleCreate(newRuleView(tt.rule, map[string]string{"prov_acme": "github-actions:acme"}))
			covers := strings.Contains(stderr.String(), "covers every repository of acme, also the repositories "+
				"that the SafeDep GitHub App installation does not select")
			assert.Equal(t, tt.wantCover, covers, stderr.String())
		})
	}
}
