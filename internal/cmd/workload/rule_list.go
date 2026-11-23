package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

type ruleListInput struct {
	Provider  string
	PageSize  uint32
	PageToken string
}

type ruleListResult struct {
	Rules         []ruleView `json:"rules"`
	NextPageToken string     `json:"next_page_token,omitempty"`
}

func ruleListCmd(a *app.App) *cobra.Command {
	var in ruleListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workload trust rules",
		Long: "List the workload trust rules of the tenant, oldest first, with the repositories and the " +
			"conditions of each rule. --provider takes a provider id or KIND:ACCOUNT, as in " +
			"github-actions:acme. The command reads one page. Pass --page-token to read the next page.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			res, err := runRuleList(cmd.Context(), client, in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Provider, "provider", "", "filter: rules of this provider, as in github-actions:acme")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size, or the server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

// runRuleList reads the providers first, to resolve --provider and to show
// each rule with the KIND:ACCOUNT name of its provider.
func runRuleList(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	in ruleListInput,
) (*ruleListResult, error) {
	providers, err := listAllProviders(ctx, client, "rule list")
	if err != nil {
		return nil, err
	}
	req := &controltowerv1.ListWorkloadTrustRulesRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if in.Provider != "" {
		provider, err := resolveProvider(providers, in.Provider)
		if err != nil {
			return nil, err
		}
		filters := &controltowerv1.ListWorkloadTrustRulesRequest_Filters{}
		filters.SetProviderId(provider.GetProviderId())
		req.SetFilters(filters)
	}
	res, err := client.ListWorkloadTrustRules(ctx, req)
	if err != nil {
		return nil, mapError("rule list", err)
	}
	names := providerNames(providers)
	out := &ruleListResult{
		Rules:         make([]ruleView, 0, len(res.GetRules())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, r := range res.GetRules() {
		out.Rules = append(out.Rules, newRuleView(r, names))
	}
	return out, nil
}

func (r *ruleListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *ruleListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Rules))
	for _, rule := range r.Rules {
		rows = append(rows, []string{
			rule.ID, rule.Name, rule.provider(), rule.Grant, rule.repositorySummary(),
			cloudpolicy.EmptyDash(strings.Join(rule.otherConditions(), " and ")), rule.lastUsedCell(now),
		})
	}
	t := table.New().
		Title("Workload trust rules").
		Headers("RULE", "NAME", "PROVIDER", "GRANT", "REPOSITORIES", "CONDITIONS", "LAST USED").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No workload trust rules. Create one with: safedep workload rule create NAME --provider PROVIDER --grant tool")
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
	b.WriteString("rule_id\tname\tprovider\tprovider_id\tgrant\trepositories\tconditions\tproject_binding\tlast_used_at")
	for _, rule := range r.Rules {
		b.WriteString("\n" + tui.PlainRow(rule.ID, rule.Name, rule.Provider, rule.ProviderID, rule.Grant,
			rule.repositoryPairs(), strings.Join(rule.otherConditions(), " and "),
			rule.ProjectBinding, rule.LastUsedAt))
	}
	return b.String()
}
