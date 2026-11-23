package workload

import (
	"context"
	"strings"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	clitui "github.com/safedep/cli/internal/tui"
)

type ruleCreateInput struct {
	Name         string
	Provider     string
	Grant        string
	Repositories []string
	Refs         []string
	Events       []string
}

func ruleCreateCmd(a *app.App) *cobra.Command {
	var in ruleCreateInput
	cmd := &cobra.Command{
		Use:   "create NAME --provider PROVIDER --grant GRANT",
		Short: "Create a workload trust rule",
		Long: "Create a workload trust rule. The rule gives the grant to the CI jobs of the provider whose " +
			"claims meet every condition of the rule. --provider is required: the CLI never picks a provider, " +
			"and with no --provider the command lists the providers of the tenant. --repository names a " +
			"repository by its full name. The server finds it in the account of the provider and stores its " +
			"numeric id, so a rename does not change the rule. A rule with no --repository covers every " +
			"repository of the account. --ref takes a full ref, as in refs/heads/main, or a prefix that ends " +
			"with /*, as in refs/heads/release/*. A rule with no --event covers only the default events of " +
			"the provider. For GitHub Actions they are push, pull_request, merge_group, schedule, " +
			"workflow_dispatch and release.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			in.Name = args[0]
			_, err := newRuleCreateRequest(in)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			res, err := runRuleCreate(cmd.Context(), client, in)
			if err != nil {
				return err
			}
			reportRuleCreate(res.Rule)
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Provider, "provider", "", "the provider of the rule: a provider id or KIND:ACCOUNT, as in github-actions:acme (required)")
	f.StringVar(&in.Grant, "grant", "", "the grant that the rule gives: "+strings.Join(grantTokens(), ", ")+" (required)")
	f.StringSliceVar(&in.Repositories, "repository", nil, "a repository by full name, as in acme/web. Repeat it for more repositories")
	f.StringArrayVar(&in.Refs, "ref", nil, "a full ref, as in refs/heads/main, or a prefix, as in refs/heads/release/*. Repeat it for more refs")
	f.StringSliceVar(&in.Events, "event", nil, "an event name, as in push or pull_request_target. Repeat it for more events")
	return cmd
}

func runRuleCreate(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	in ruleCreateInput,
) (*ruleResult, error) {
	req, err := newRuleCreateRequest(in)
	if err != nil {
		return nil, err
	}
	providers, err := listAllProviders(ctx, client, "rule create")
	if err != nil {
		return nil, err
	}
	provider, err := resolveProvider(providers, in.Provider)
	if err != nil {
		return nil, err
	}
	req.SetProviderId(provider.GetProviderId())
	res, err := client.CreateWorkloadTrustRule(ctx, req)
	if err != nil {
		return nil, mapRuleCreateError(in.Name, providerName(provider), err)
	}
	return &ruleResult{Rule: newRuleView(res.GetRule(), providerNames(providers))}, nil
}

func reportRuleCreate(rule ruleView) {
	tui.Success("rule %s created", clitui.EscapeLine(rule.Name))
	if rule.EveryRepository {
		tui.Warning("Rule %s covers every repository of %s, also the repositories that the SafeDep GitHub App "+
			"installation does not select. Pass --repository to name repositories",
			clitui.EscapeLine(rule.Name), clitui.EscapeLine(rule.account()))
	}
}
