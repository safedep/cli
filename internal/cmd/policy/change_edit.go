package policy

import (
	"context"
	"errors"
	"fmt"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

func changeEditCmd(a *app.App) *cobra.Command {
	var in editInput
	cmd := &cobra.Command{
		Use:   "edit CHANGE",
		Short: "Edit the policy settings of an open policy change",
		Long: "Open the policy settings of an open policy change in $VISUAL or $EDITOR. Saving updates the " +
			"change on the server. A change that does not edit the settings yet starts from the head " +
			"settings. Use -m to change the title, and --override NAME to edit one policy override.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireTerminal(); err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			change, err := runChangeEdit(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()),
				openEditor, a.Tenant(), args[0], in)
			if errors.Is(err, errNoChange) {
				tui.Info("No change. Nothing is saved")
				return nil
			}
			if err != nil {
				return err
			}
			id := clitui.EscapeLine(change.GetChangeId())
			tui.Success("change %s updated", id)
			tui.Info("  Review it: safedep policy change show %s", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&in.Title, "message", "m", "", "a new title for the policy change")
	cmd.Flags().StringVar(&in.Override, "override", "", "edit only the policy override with this name")
	return cmd
}

func runChangeEdit(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	open openEditorFunc,
	tenant, id string,
	in editInput,
) (*policyv1.PolicyChange, error) {
	change, err := getChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if err := requireOpen(change); err != nil {
		return nil, err
	}
	head, err := getPolicy(ctx, client)
	if err != nil {
		return nil, err
	}

	start := head.GetPolicy()
	if change.HasSettings() {
		start = change.GetSettings()
	}
	if change.HasRestore() {
		tui.Warning("Change %s restores from revision %d. Saving replaces the settings that the restore made",
			clitui.EscapeLine(id), change.GetRestore().GetFromRevision())
	}
	intro := []string{
		fmt.Sprintf("Change %s of %s%sbase revision %d%shead revision %d",
			id, tenantName(tenant), sep, change.GetBaseRevision(), sep, head.GetHeadRevision()),
		"Saving updates the draft change. Nothing applies until you publish it.",
	}

	var updated *policyv1.PolicyChange
	err = editSettings(ctx, open, start, editHeader(intro, head), defaultLayer(head), in.Override, func(p *policyv1.Policy) error {
		req := &policyv1.UpdatePolicyChangeRequest{}
		req.SetChangeId(id)
		req.SetEtag(change.GetEtag())
		req.SetSettings(p)
		if in.Title != "" {
			req.SetTitle(in.Title)
		}
		res, err := client.UpdatePolicyChange(ctx, req)
		if err != nil {
			return mapChangeError("update", id, err)
		}
		updated = res.GetChange()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
