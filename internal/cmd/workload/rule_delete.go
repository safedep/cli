package workload

import (
	"context"
	"errors"
	"fmt"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/prompt"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	clitui "github.com/safedep/cli/internal/tui"
)

// confirmFunc asks the user to confirm an action and returns the answer.
type confirmFunc func(question string) (bool, error)

// errDeleteCancelled stops a delete that the user did not confirm.
var errDeleteCancelled = errors.New("delete cancelled")

func ruleDeleteCmd(a *app.App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete RULE",
		Short: "Delete a workload trust rule",
		Long: "Delete a workload trust rule by its id or its name. Every workload credential that the rule " +
			"gave stops working within 30 seconds, and the CI jobs that the rule admits lose access to SafeDep " +
			"Cloud. The command asks for a confirmation in a terminal, and needs --yes when no terminal is " +
			"present. The exchanges of the rule stay in `safedep workload exchange list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			var confirm confirmFunc
			if !yes {
				confirm = terminalConfirm(clitui.IsInteractive(), args[0])
			}
			id, err := runRuleDelete(cmd.Context(), client, args[0], confirm)
			if errors.Is(err, errDeleteCancelled) {
				tui.Info("Nothing is deleted")
				return nil
			}
			if err != nil {
				return err
			}
			tui.Success("rule %s deleted", clitui.EscapeLine(id))
			tui.Info("Every workload credential that the rule gave stops working within 30 seconds")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete the rule with no prompt")
	return cmd
}

// terminalConfirm asks in a terminal. With no terminal it refuses, because a
// script that deletes a rule must say so with --yes.
func terminalConfirm(interactive bool, ref string) confirmFunc {
	return func(question string) (bool, error) {
		if !interactive {
			problem := "the delete of a workload trust rule needs a confirmation, and no terminal is present to confirm it"
			return false, newWorkloadError(usefulerror.ErrBadRequest, problem,
				fmt.Sprintf("Review it with safedep workload rule show %s, then pass --yes to delete it", clitui.EscapeLine(ref)),
				errors.New(problem))
		}
		return prompt.Confirm(question, false)
	}
}

// runRuleDelete deletes the rule that ref names and returns its id. A nil
// confirm means that the user gave --yes, so the command reads nothing to
// ask about.
func runRuleDelete(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	ref string,
	confirm confirmFunc,
) (string, error) {
	id, _, err := ruleIDOf(ctx, client, "rule delete", ref)
	if err != nil {
		return "", err
	}
	if confirm != nil {
		if err := confirmRuleDelete(ctx, client, id, ref, confirm); err != nil {
			return "", err
		}
	}
	req := &controltowerv1.DeleteWorkloadTrustRuleRequest{}
	req.SetRuleId(id)
	if _, err := client.DeleteWorkloadTrustRule(ctx, req); err != nil {
		return "", mapRuleError("rule delete", ref, err)
	}
	return id, nil
}

// confirmRuleDelete reads the rule, so the question says which jobs lose
// access: every repository of the account for a rule with no repository
// condition.
func confirmRuleDelete(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	id, ref string,
	confirm confirmFunc,
) error {
	get := &controltowerv1.GetWorkloadTrustRuleRequest{}
	get.SetRuleId(id)
	res, err := client.GetWorkloadTrustRule(ctx, get)
	if err != nil {
		return mapRuleError("rule delete", ref, err)
	}
	providers, err := listAllProviders(ctx, client, "rule delete")
	if err != nil {
		return err
	}
	ok, err := confirm(deleteQuestion(newRuleView(res.GetRule(), providerNames(providers))))
	if err != nil {
		return err
	}
	if !ok {
		return errDeleteCancelled
	}
	return nil
}

func deleteQuestion(rule ruleView) string {
	name := clitui.EscapeLine(rule.Name)
	if rule.EveryRepository {
		return fmt.Sprintf("Rule %s covers every repository of %s. The CI jobs of every repository that it "+
			"admits lose access to SafeDep Cloud. Delete it?", name, clitui.EscapeLine(rule.account()))
	}
	return fmt.Sprintf("The CI jobs that rule %s admits, in %s, lose access to SafeDep Cloud. Delete it?",
		name, clitui.EscapeLine(rule.repositorySummary()))
}
