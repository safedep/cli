package policy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

const (
	packageRulesKeepHead     = "head"
	packageRulesFromRevision = "revision"
)

type changeCreateInput struct {
	FromRevision uint64
	Capabilities []string
	PackageRules string
	Title        string
}

func changeCreateCmd(a *app.App) *cobra.Command {
	var in changeCreateInput
	cmd := &cobra.Command{
		Use:   "create --from-revision REVISION",
		Short: "Create a policy change that restores an old revision",
		Long: "Create a policy change that restores capabilities from an old policy revision, for a " +
			"rollback. For each restored capability, the policy base and every policy override take " +
			"their values from that revision. Package rules stay at head unless you pass " +
			"--package-rules revision. The change is a draft: review it, then publish it. Revision " +
			"numbers only go up, so the publish makes a new revision.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validateChangeCreate(in)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			policyClient := cloudpolicy.NewPolicyClient(client.Connection())
			change, err := runChangeCreate(cmd.Context(), policyClient, in)
			if err != nil {
				return err
			}
			id := clitui.EscapeLine(change.GetChangeId())
			tui.Success("change %s %s", id, clitui.EscapeLine(restoreSummary(change.GetRestore())))
			reportRestoreDirection(cmd.Context(), policyClient, change.GetChangeId())
			tui.Info("  Review it: safedep policy change show %s", id)
			tui.Info("  Publish it: safedep policy change publish %s", id)
			return nil
		},
	}
	f := cmd.Flags()
	f.Uint64Var(&in.FromRevision, "from-revision", 0, "the policy revision to restore from (required)")
	f.StringSliceVar(&in.Capabilities, "capability", nil,
		"a capability to restore, one of "+strings.Join(cloudpolicy.Capabilities(), ", ")+
			" (repeatable, every capability when not set)")
	f.StringVar(&in.PackageRules, "package-rules", packageRulesKeepHead,
		"the package rules after the restore: head keeps them, revision restores the set of --from-revision")
	f.StringVarP(&in.Title, "message", "m", "", "the title of the policy change")
	return cmd
}

func validateChangeCreate(in changeCreateInput) error {
	if in.FromRevision == 0 {
		return invalidInputError(errors.New("--from-revision is required"),
			"Find the revision in: safedep policy revision list")
	}
	known := cloudpolicy.Capabilities()
	for _, c := range in.Capabilities {
		if !slices.Contains(known, c) {
			return invalidInputError(
				fmt.Errorf("unknown --capability %q: known capabilities are %s", c, strings.Join(known, ", ")),
				"Retry with one of: "+strings.Join(known, ", "))
		}
	}
	switch in.PackageRules {
	case packageRulesKeepHead, packageRulesFromRevision:
		return nil
	default:
		return invalidInputError(
			fmt.Errorf("unknown --package-rules %q: use head or revision", in.PackageRules),
			"Retry with --package-rules head or --package-rules revision")
	}
}

func runChangeCreate(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	in changeCreateInput,
) (*policyv1.PolicyChange, error) {
	if err := validateChangeCreate(in); err != nil {
		return nil, err
	}
	restore := &policyv1.RestoreSource{}
	restore.SetFromRevision(in.FromRevision)
	if len(in.Capabilities) > 0 {
		restore.SetCapabilities(in.Capabilities)
	}
	rules := policyv1.PackageRuleRestore_PACKAGE_RULE_RESTORE_KEEP_HEAD
	if in.PackageRules == packageRulesFromRevision {
		rules = policyv1.PackageRuleRestore_PACKAGE_RULE_RESTORE_FROM_REVISION
	}
	restore.SetPackageRules(rules)

	req := &policyv1.CreatePolicyChangeRequest{}
	req.SetTitle(in.Title)
	req.SetRestore(restore)
	res, err := client.CreatePolicyChange(ctx, req)
	if err != nil {
		return nil, mapRestoreError(in.FromRevision, err)
	}
	return res.GetChange(), nil
}

// reportRestoreDirection names each field that the restore loosens, so the
// admin sees it before the publish. The change exists already, so a failed
// preview is a warning, not an error.
func reportRestoreDirection(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) {
	preview, err := previewChange(ctx, client, id)
	if err != nil {
		tui.Warning("Cannot preview change %s: %s", clitui.EscapeLine(id), clitui.EscapeLine(err.Error()))
		return
	}
	reportLoosening(preview)
}
