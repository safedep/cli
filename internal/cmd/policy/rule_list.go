package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const baseAttachment = "base"

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
			"attaches to. The command reads one page. Pass --page-token to read the next page.",
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
			rule.ID, rule.selector(), rule.Effect, cloudpolicy.EmptyDash(strings.Join(rule.Scopes, ",")),
			rule.Reason, cloudpolicy.EmptyDash(rule.RequestedBy), rule.AttachesTo, rule.expiresCell(now),
		})
	}
	t := table.New().
		Title("Package rules").
		Headers("RULE", "SELECTOR", "EFFECT", "SCOPE", "REASON", "REQUESTED BY", "ATTACHES TO", "EXPIRES").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No package rules match. Block a package with: safedep policy rule create --block pkg:npm/name --reason TEXT")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), cloudpolicy.Plural(len(rows), "rule", "rules"))
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
		b.WriteString("\n" + tui.PlainRow(rule.ID, rule.Purl, rule.Vers, rule.Effect, strings.Join(rule.Scopes, ","),
			rule.Reason, rule.RequestedBy, rule.OverrideID, rule.ExpiresAt))
	}
	return b.String()
}
