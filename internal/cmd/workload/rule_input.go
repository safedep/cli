package workload

import (
	"fmt"
	"regexp"
	"strings"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"

	clitui "github.com/safedep/cli/internal/tui"
)

// The patterns match the protovalidate rules of CreateWorkloadTrustRuleRequest
// and WorkloadClaimCondition, so a bad flag fails before any RPC with the
// flag in the message.
var (
	ruleNamePattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	refPrefixPattern = regexp.MustCompile(`^refs/(heads|tags)/([^*]*/)?[*]$`)
	eventPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
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
	return req, nil
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

// repositoryNames checks each --repository value. An owner or a name of "."
// or ".." is never a GitHub repository, and the server refuses it.
func repositoryNames(values []string) ([]string, error) {
	for _, v := range values {
		owner, name, ok := strings.Cut(v, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") ||
			isDotSegment(owner) || isDotSegment(name) {
			return nil, invalidInputError(fmt.Sprintf("--repository %q: write OWNER/NAME", v),
				"As in --repository acme/web")
		}
	}
	return unique(values), nil
}

func isDotSegment(s string) bool {
	return s == "." || s == ".."
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
