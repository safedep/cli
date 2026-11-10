package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

func ruleDeleteCmd(a *app.App) *cobra.Command {
	var (
		reason, message string
		yes             bool
	)
	cmd := &cobra.Command{
		Use:   "delete RULE --reason TEXT",
		Short: "Delete a package rule and publish at once",
		Long: "Delete a package rule and publish the change at once as a new policy revision. The rule " +
			"stays in the history of the revisions. The delete of a block entry loosens the policy, so it " +
			"needs a confirmation: the command asks in a terminal, and needs --yes when no terminal is present.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return invalidInputError(errors.New("--reason is required"), "Say why the rule goes, as in --reason \"no longer needed\"")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			confirm := publishConfirm(yes, clitui.IsInteractive(),
				"the delete of a block entry loosens the policy, and no terminal is present to confirm it",
				"Review it with safedep policy rule show, then pass --yes to delete it")
			rev, err := runRuleDelete(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0], reason, message, confirm)
			if errors.Is(err, errPublishCancelled) {
				tui.Info("Nothing is deleted")
				return nil
			}
			if err != nil {
				return err
			}
			tui.Success("rule %s deleted%srevision %d published", clitui.EscapeLine(args[0]), sep, rev)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the rule goes (required)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "the publish message, or a message that the server writes when empty")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete a block entry with no prompt")
	return cmd
}

// runRuleDelete reads the rule first, because only the delete of a block
// entry loosens the policy and needs a confirmation.
func runRuleDelete(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	id, reason, message string,
	confirm confirmFunc,
) (uint64, error) {
	get := &policyv1.GetPackageRuleRequest{}
	get.SetRuleId(id)
	rule, err := client.GetPackageRule(ctx, get)
	if err != nil {
		return 0, mapRuleError("delete", id, err)
	}
	if r := rule.GetRule().GetRule(); r.GetEffect() == corev1.Effect_EFFECT_BLOCK {
		ok, err := confirm(fmt.Sprintf("Rule %s blocks %s. The delete loosens the policy. Delete it?",
			clitui.EscapeLine(id), clitui.EscapeLine(r.GetSelector().GetPurl())))
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, errPublishCancelled
		}
	}
	del := &policyv1.PackageRuleDelete{}
	del.SetRuleId(id)
	del.SetReason(reason)
	req := &policyv1.DeletePackageRuleRequest{}
	req.SetDelete(del)
	req.SetMessage(message)
	res, err := client.DeletePackageRule(ctx, req)
	if err != nil {
		return 0, mapRuleError("delete", id, err)
	}
	return res.GetRevision().GetRevision(), nil
}
