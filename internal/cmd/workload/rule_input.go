package workload

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/protobuf/types/known/durationpb"

	clitui "github.com/safedep/cli/internal/tui"
)

// The name, ref prefix and repository patterns and the lifetime bounds match
// the protovalidate rules of CreateWorkloadTrustRuleRequest and
// WorkloadClaimCondition, so a bad flag fails before any RPC with the flag in
// the message. The event pattern is the form of a GitHub Actions event name:
// a value of another form never matches a token.
var (
	ruleNamePattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	refPrefixPattern = regexp.MustCompile(`^refs/(heads|tags)/([^*]*/)?[*]$`)
	eventPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	// repositoryPattern holds at least one character that is not a dot in
	// each part, so neither part is "." or "..".
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]*[A-Za-z0-9_-][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]*[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)
)

const (
	maxRepositoryLength = 256
	maxRepositories     = 100
	minMaxLifetime      = time.Minute
	maxMaxLifetime      = 6 * time.Hour
)

func grantTokens() []string {
	return clitui.EnumTokens(messagescontroltowerv1.CredentialGrant_name, grantPrefix)
}

// newRuleCreateRequest checks the flags and builds the request with no
// provider id. The provider id needs the providers of the tenant.
func newRuleCreateRequest(in ruleCreateInput) (*controltowerv1.CreateWorkloadTrustRuleRequest, error) {
	if !ruleNamePattern.MatchString(in.Name) {
		return nil, invalidInputError(
			fmt.Sprintf("rule name %q: use 1 to 63 lowercase letters, digits and hyphens, with a letter or a digit at each end", in.Name),
			"As in: safedep workload rule create org or safedep workload rule create web-release")
	}
	grant, err := parseGrant(in.Grant)
	if err != nil {
		return nil, err
	}
	repositories, err := repositoryNames(in.Repositories)
	if err != nil {
		return nil, err
	}
	var conditions []*controltowerv1.WorkloadClaimCondition
	ref, err := refCondition(in.Refs)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		conditions = append(conditions, ref)
	}
	event, err := eventCondition(in.Events)
	if err != nil {
		return nil, err
	}
	if event != nil {
		conditions = append(conditions, event)
	}

	req := &controltowerv1.CreateWorkloadTrustRuleRequest{}
	req.SetName(in.Name)
	req.SetGrant(grant)
	req.SetRepositories(repositories)
	req.SetConditions(conditions)
	if in.ProjectBinding != "" {
		binding, err := parseProjectBinding(in.ProjectBinding)
		if err != nil {
			return nil, err
		}
		req.SetProjectBinding(binding)
	}
	if in.MaxLifetimeSet {
		if in.MaxLifetime < minMaxLifetime || in.MaxLifetime > maxMaxLifetime {
			return nil, invalidInputError(fmt.Sprintf("--max-lifetime %s: use a duration from 1m to 6h", in.MaxLifetime),
				"As in --max-lifetime 30m or --max-lifetime 2h")
		}
		req.SetMaxLifetime(durationpb.New(in.MaxLifetime))
	}
	return req, nil
}

func projectBindingTokens() []string {
	return clitui.EnumTokens(controltowerv1.WorkloadProjectBinding_name, projectBindingPrefix)
}

func parseProjectBinding(token string) (controltowerv1.WorkloadProjectBinding, error) {
	n, ok := clitui.ParseEnumToken(controltowerv1.WorkloadProjectBinding_name, projectBindingPrefix, token)
	if !ok {
		return 0, invalidInputError(fmt.Sprintf("--project-binding %q is not a project binding", token),
			"Pass --project-binding "+strings.Join(projectBindingTokens(), " or --project-binding "))
	}
	return controltowerv1.WorkloadProjectBinding(n), nil
}

func parseGrant(token string) (messagescontroltowerv1.CredentialGrant, error) {
	help := "Pass --grant " + strings.Join(grantTokens(), " or --grant ")
	if token == "" {
		return 0, invalidInputError("--grant is required", help)
	}
	n, ok := clitui.ParseEnumToken(messagescontroltowerv1.CredentialGrant_name, grantPrefix, token)
	if !ok {
		return 0, invalidInputError(fmt.Sprintf("--grant %q is not a grant", token), help)
	}
	return messagescontroltowerv1.CredentialGrant(n), nil
}

// repositoryNames checks each --repository value against the protovalidate
// rules of CreateWorkloadTrustRuleRequest.repositories.
func repositoryNames(values []string) ([]string, error) {
	for _, v := range values {
		if len(v) > maxRepositoryLength || !repositoryPattern.MatchString(v) {
			return nil, invalidInputError(fmt.Sprintf("--repository %q: write OWNER/NAME with letters, digits, "+
				"'-', '_' and '.', in at most %d characters", v, maxRepositoryLength),
				"As in --repository acme/web")
		}
	}
	names := unique(values)
	if len(names) > maxRepositories {
		return nil, invalidInputError(fmt.Sprintf("--repository: a rule names at most %d repositories", maxRepositories),
			"Split the repositories over more rules, or create a rule with no --repository for every repository")
	}
	return names, nil
}

// refCondition builds the condition on the claim ref. A rule has at most one
// condition for each claim, and a condition has one operator, so the values
// are all full refs or all prefixes.
func refCondition(refs []string) (*controltowerv1.WorkloadClaimCondition, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	prefixes := 0
	for _, ref := range refs {
		if strings.Contains(ref, "*") {
			if !refPrefixPattern.MatchString(ref) {
				return nil, invalidInputError(fmt.Sprintf("--ref %q: a prefix is refs/heads/ or refs/tags/, then path "+
					"parts that end with /, then one *", ref),
					"As in --ref refs/heads/release/*. A prefix that ends in a name, as refs/heads/main*, "+
						"also matches refs/heads/main-evil")
			}
			prefixes++
			continue
		}
		if !strings.HasPrefix(ref, "refs/") {
			// A GitHub token holds the full ref, so a short name such as main
			// would make a rule that never matches.
			return nil, invalidInputError(fmt.Sprintf("--ref %q: write the full ref", ref),
				"As in --ref refs/heads/main or --ref refs/tags/v1.2.0")
		}
	}
	operator := controltowerv1.ClaimConditionOperator_CLAIM_CONDITION_OPERATOR_EQUALS
	switch prefixes {
	case 0:
	case len(refs):
		operator = controltowerv1.ClaimConditionOperator_CLAIM_CONDITION_OPERATOR_PREFIX
	default:
		return nil, invalidInputError("--ref takes full refs or prefixes, not both",
			"Create one rule for the full refs and one rule for the prefixes")
	}
	return newCondition(claimRef, operator, refs), nil
}

func eventCondition(events []string) (*controltowerv1.WorkloadClaimCondition, error) {
	if len(events) == 0 {
		return nil, nil
	}
	for _, e := range events {
		if !eventPattern.MatchString(e) {
			return nil, invalidInputError(fmt.Sprintf("--event %q: write the event name of the platform", e),
				"As in --event push or --event pull_request_target")
		}
	}
	return newCondition(claimEventName, controltowerv1.ClaimConditionOperator_CLAIM_CONDITION_OPERATOR_EQUALS, events), nil
}

func newCondition(claim string, operator controltowerv1.ClaimConditionOperator, values []string) *controltowerv1.WorkloadClaimCondition {
	c := &controltowerv1.WorkloadClaimCondition{}
	c.SetClaim(claim)
	c.SetOperator(operator)
	c.SetValues(unique(values))
	return c
}

// unique drops repeated values and keeps the order. The server refuses a
// list with a repeated value.
func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
