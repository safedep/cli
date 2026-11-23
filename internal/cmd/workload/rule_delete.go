package workload

import (
	"context"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	clitui "github.com/safedep/cli/internal/tui"
)

func ruleDeleteCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "delete RULE",
		Short: "Delete a workload trust rule",
		Long: "Delete a workload trust rule by its id or its name. Every workload credential that the rule " +
			"gave stops working within 30 seconds, and the CI jobs that the rule covered get no new credential. " +
			"The exchanges of the rule stay in `safedep workload exchange list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			id, err := runRuleDelete(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			tui.Success("rule %s deleted", clitui.EscapeLine(id))
			tui.Info("Every workload credential that the rule gave stops working within 30 seconds")
			return nil
		},
	}
}

// runRuleDelete deletes the rule that ref names and returns its id.
func runRuleDelete(ctx context.Context, client controltowerv1grpc.WorkloadAdminServiceClient, ref string) (string, error) {
	id, err := ruleIDOf(ctx, client, "rule delete", ref)
	if err != nil {
		return "", err
	}
	req := &controltowerv1.DeleteWorkloadTrustRuleRequest{}
	req.SetRuleId(id)
	if _, err := client.DeleteWorkloadTrustRule(ctx, req); err != nil {
		return "", mapRuleError("rule delete", ref, err)
	}
	return id, nil
}
