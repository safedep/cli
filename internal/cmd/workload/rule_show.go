package workload

import (
	"context"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/paging"
)

func ruleShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show RULE",
		Short: "Show one workload trust rule",
		Long: "Show one workload trust rule by its id or its name: the provider, the grant, each repository " +
			"with its numeric id, the conditions, how a repository with no project gets a project, the " +
			"longest life of a credential, and when the rule last gave a credential.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			res, err := runRuleShow(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
}

func runRuleShow(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	ref string,
) (*ruleResult, error) {
	id, err := ruleIDOf(ctx, client, "rule show", ref)
	if err != nil {
		return nil, err
	}
	req := &controltowerv1.GetWorkloadTrustRuleRequest{}
	req.SetRuleId(id)
	res, err := client.GetWorkloadTrustRule(ctx, req)
	if err != nil {
		return nil, mapRuleError("rule show", ref, err)
	}
	providers, err := listAllProviders(ctx, client, "rule show")
	if err != nil {
		return nil, err
	}
	return &ruleResult{Rule: newRuleView(res.GetRule(), providerNames(providers))}, nil
}

// ruleIDOf finds the id of the rule that ref names: a rule id, or else the
// name of a live rule. A ref that names no live rule stays as it is, so the
// server decides. ListWorkloadExchanges finds the exchanges of a deleted
// rule by its id.
func ruleIDOf(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	command, ref string,
) (string, error) {
	id, named := "", ""
	err := paging.Paginate(ctx, "workload "+command, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListWorkloadTrustRulesRequest{}
		req.SetPagination(paging.NewPaginationRequest(0, pageToken))
		res, err := client.ListWorkloadTrustRules(ctx, req)
		if err != nil {
			return "", mapError(command, err)
		}
		for _, r := range res.GetRules() {
			if r.GetRuleId() == ref {
				id = ref
				return "", nil
			}
			if r.GetName() == ref && named == "" {
				named = r.GetRuleId()
			}
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return "", err
	}
	switch {
	case id != "":
		return id, nil
	case named != "":
		return named, nil
	}
	return ref, nil
}
