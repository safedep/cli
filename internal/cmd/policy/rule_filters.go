package policy

import (
	"fmt"
	"slices"
	"strings"

	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/safedep/cli/internal/tui"
)

func trustScopeTokens() []string {
	return tui.EnumTokens(corev1.TrustScope_name, trustScopePrefix)
}

// createScopeTokens are the values of --scope in a create. A trust entry
// gets NAMED_VULNERABILITY from --scope vulnerability with --vulnerability,
// because that scope needs ids. So "all" leaves it out too.
func createScopeTokens() []string {
	named := tui.EnumToken(corev1.TrustScope_TRUST_SCOPE_NAMED_VULNERABILITY.String(), trustScopePrefix)
	return slices.DeleteFunc(trustScopeTokens(), func(token string) bool { return token == named })
}

func parseEffect(token string) (corev1.Effect, error) {
	n, ok := tui.ParseEnumToken(corev1.Effect_name, effectPrefix, token)
	if !ok {
		return 0, invalidInputError(fmt.Errorf("unknown --effect value %q: use trust or block", token),
			"Retry with --effect trust or --effect block")
	}
	return corev1.Effect(n), nil
}

// parseTrustScope reads one of the allowed tokens.
func parseTrustScope(token string, allowed []string) (corev1.TrustScope, error) {
	normalized := strings.ToLower(strings.TrimSpace(token))
	n, ok := tui.ParseEnumToken(corev1.TrustScope_name, trustScopePrefix, normalized)
	if !ok || !slices.Contains(allowed, normalized) {
		return 0, invalidInputError(
			fmt.Errorf("unknown trust scope %q: allowed values are %s", token, strings.Join(allowed, ", ")),
			"Retry with one of: "+strings.Join(allowed, ", "))
	}
	return corev1.TrustScope(n), nil
}

// ruleListFilters builds the filters. overrideID is the id of in.Override,
// which the caller resolves.
func ruleListFilters(in *ruleListInput, overrideID string) (*policyv1.ListPackageRulesRequest_Filters, error) {
	if in.Override != "" && in.Base {
		return nil, invalidInputError(fmt.Errorf("--override and --base select different rules"), "Use one of them")
	}
	filters := &policyv1.ListPackageRulesRequest_Filters{}
	set := false
	if in.Effect != "" {
		effect, err := parseEffect(in.Effect)
		if err != nil {
			return nil, err
		}
		filters.SetEffect(effect)
		set = true
	}
	if in.Scope != "" {
		scope, err := parseTrustScope(in.Scope, trustScopeTokens())
		if err != nil {
			return nil, err
		}
		filters.SetScope(scope)
		set = true
	}
	if in.SelectorPrefix != "" {
		filters.SetSelectorPrefix(in.SelectorPrefix)
		set = true
	}
	if in.Expiring < 0 {
		return nil, invalidInputError(fmt.Errorf("--expiring must not be negative"), "Use a duration such as 7d")
	}
	if in.Expiring > 0 {
		filters.SetExpiringWithin(durationpb.New(in.Expiring))
		set = true
	}
	if in.Base {
		filters.SetOverrideId("")
		set = true
	}
	if overrideID != "" {
		filters.SetOverrideId(overrideID)
		set = true
	}
	if in.AtRevision > 0 {
		filters.SetAtRevision(in.AtRevision)
		set = true
	}
	if !set {
		return nil, nil
	}
	return filters, nil
}
