package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const baseAttachment = "base"

type ruleView struct {
	ID              string   `json:"rule_id"`
	Purl            string   `json:"purl"`
	Vers            string   `json:"vers,omitempty"`
	Effect          string   `json:"effect"`
	Scopes          []string `json:"scopes"`
	Reason          string   `json:"reason"`
	Ticket          string   `json:"ticket,omitempty"`
	RequestedBy     string   `json:"requested_by,omitempty"`
	OverrideID      string   `json:"override_id,omitempty"`
	AttachesTo      string   `json:"attaches_to"`
	CreatedBy       string   `json:"created_by,omitempty"`
	AddedRevision   uint64   `json:"added_revision"`
	RemovedRevision uint64   `json:"removed_revision,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	CreatedAt       string   `json:"created_at,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	expires         *time.Time
}

// newRuleView translates a rule. names maps override ids to names. A rule
// whose override is not in names shows the override id.
func newRuleView(r *policyv1.PackageRuleWithAttributes, names map[string]string) ruleView {
	rule := r.GetRule()
	expires := timeOf(rule.GetExpiresAt())
	v := ruleView{
		ID:              rule.GetRuleId(),
		Purl:            rule.GetSelector().GetPurl(),
		Vers:            rule.GetSelector().GetVers(),
		Effect:          tui.EnumToken(rule.GetEffect().String(), effectPrefix),
		Scopes:          scopeTokens(rule.GetScopes()),
		Reason:          rule.GetReason(),
		Ticket:          r.GetTicket(),
		RequestedBy:     r.GetRequestedBy(),
		OverrideID:      r.GetOverrideId(),
		AttachesTo:      baseAttachment,
		CreatedBy:       actorName(r.GetCreatedBy()),
		AddedRevision:   r.GetAddedRevision(),
		RemovedRevision: r.GetRemovedRevision(),
		ExpiresAt:       cloudpolicy.RFC3339(expires),
		CreatedAt:       cloudpolicy.RFC3339(timeOf(r.GetCreatedAt())),
		UpdatedAt:       cloudpolicy.RFC3339(timeOf(r.GetUpdatedAt())),
		expires:         expires,
	}
	if id := r.GetOverrideId(); id != "" {
		v.AttachesTo = "override " + id
		if name, ok := names[id]; ok {
			v.AttachesTo = fmt.Sprintf("override %q", name)
		}
	}
	return v
}

func (v ruleView) selector() string {
	if v.Vers == "" {
		return v.Purl
	}
	return v.Purl + " " + v.Vers
}

func (v ruleView) expiresCell(now time.Time) string {
	if v.expires == nil {
		return "never"
	}
	return humanize.Time(*v.expires, now)
}

type ruleListInput struct {
	Effect         string
	Scope          string
	SelectorPrefix string
	Expiring       time.Duration
	Override       string
	Base           bool
	AtRevision     uint64
	PageSize       uint32
	PageToken      string
}

type ruleListResult struct {
	Rules         []ruleView `json:"rules"`
	NextPageToken string     `json:"next_page_token,omitempty"`
}

func ruleListCmd(a *app.App) *cobra.Command {
	var in ruleListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List package rules",
		Long: "List the active package rules at head, or at an older policy revision with --at-revision. " +
			"Filter by effect, trust scope, selector prefix, expiry, or the policy override that a rule " +
			"attaches to. One page is returned per invocation, so follow --page-token to read the next page.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			_, err := ruleListFilters(&in, "")
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runRuleList(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), &in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Effect, "effect", "", "filter: trust or block")
	f.StringVar(&in.Scope, "scope", "", "filter: trust scope ("+strings.Join(trustScopeTokens(), ", ")+")")
	f.StringVar(&in.SelectorPrefix, "selector-prefix", "", "filter: rules whose selector starts with this text, as in pkg:npm/")
	f.Var(duration.NewValue(&in.Expiring, 0), "expiring", "filter: rules that expire within this time from now, as in 7d")
	f.StringVar(&in.Override, "override", "", "filter: rules that attach to the policy override with this name")
	f.BoolVar(&in.Base, "base", false, "filter: rules that attach to the policy base")
	f.Uint64Var(&in.AtRevision, "at-revision", 0, "list the rules that were active at this policy revision")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func trustScopeTokens() []string {
	return tui.EnumTokens(corev1.TrustScope_name, trustScopePrefix)
}

func parseEffect(token string) (corev1.Effect, error) {
	n, ok := tui.ParseEnumToken(corev1.Effect_name, effectPrefix, token)
	if !ok {
		return 0, invalidInputError(fmt.Errorf("unknown --effect value %q: use trust or block", token),
			"Retry with --effect trust or --effect block")
	}
	return corev1.Effect(n), nil
}

func parseTrustScope(token string) (corev1.TrustScope, error) {
	n, ok := tui.ParseEnumToken(corev1.TrustScope_name, trustScopePrefix, strings.ToLower(strings.TrimSpace(token)))
	if !ok {
		return 0, invalidInputError(
			fmt.Errorf("unknown trust scope %q: allowed values are %s", token, strings.Join(trustScopeTokens(), ", ")),
			"Retry with one of: "+strings.Join(trustScopeTokens(), ", "))
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
		scope, err := parseTrustScope(in.Scope)
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

func runRuleList(ctx context.Context, client policyv1grpc.PolicyServiceClient, in *ruleListInput) (*ruleListResult, error) {
	var names map[string]string
	overrideID := ""
	if in.Override != "" {
		head, err := getPolicy(ctx, client)
		if err != nil {
			return nil, err
		}
		o, err := findOverride(head.GetPolicy(), in.Override)
		if err != nil {
			return nil, err
		}
		overrideID = o.GetOverrideId()
		names = overrideNames(head.GetPolicy())
	}
	filters, err := ruleListFilters(in, overrideID)
	if err != nil {
		return nil, err
	}
	req := &policyv1.ListPackageRulesRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if filters != nil {
		req.SetFilters(filters)
	}
	res, err := client.ListPackageRules(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("policy rule list: %w", err)
	}

	if names == nil && attachesToOverride(res.GetRules()) {
		head, err := getPolicy(ctx, client)
		if err != nil {
			return nil, err
		}
		names = overrideNames(head.GetPolicy())
	}
	out := &ruleListResult{
		Rules:         make([]ruleView, 0, len(res.GetRules())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, r := range res.GetRules() {
		out.Rules = append(out.Rules, newRuleView(r, names))
	}
	return out, nil
}

func attachesToOverride(rules []*policyv1.PackageRuleWithAttributes) bool {
	for _, r := range rules {
		if r.GetOverrideId() != "" {
			return true
		}
	}
	return false
}

func (r *ruleListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *ruleListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Rules))
	for _, rule := range r.Rules {
		rows = append(rows, []string{
			rule.ID, rule.selector(), rule.Effect, emptyDash(strings.Join(rule.Scopes, ",")),
			rule.Reason, emptyDash(rule.RequestedBy), rule.AttachesTo, rule.expiresCell(now),
		})
	}
	t := table.New().
		Title("Package rules").
		Headers("RULE", "SELECTOR", "EFFECT", "SCOPE", "REASON", "REQUESTED BY", "ATTACHES TO", "EXPIRES").
		Rows(rows...).
		EmptyMessage("No package rules match. Block a package with: safedep policy rule create --block pkg:npm/name --reason TEXT")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), plural(len(rows), "rule", "rules"))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *ruleListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("rule_id\tpurl\tvers\teffect\tscopes\treason\trequested_by\toverride_id\texpires_at")
	for _, rule := range r.Rules {
		fmt.Fprintf(&b, "\n%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s",
			rule.ID, rule.Purl, rule.Vers, rule.Effect, strings.Join(rule.Scopes, ","),
			rule.Reason, rule.RequestedBy, rule.OverrideID, rule.ExpiresAt)
	}
	return b.String()
}
