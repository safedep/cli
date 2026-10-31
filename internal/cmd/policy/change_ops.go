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
			tui.Success("change %s updated", change.GetChangeId())
			tui.Info("  Review it: safedep policy change show %s", change.GetChangeId())
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
			id, change.GetRestore().GetFromRevision())
	}
	intro := []string{
		fmt.Sprintf("Change %s of %s%sbase revision %d%shead revision %d",
			id, tenantName(tenant), sep, change.GetBaseRevision(), sep, head.GetHeadRevision()),
		"Saving updates the draft change. Nothing applies until you publish it.",
	}

	var updated *policyv1.PolicyChange
	err = editSettings(ctx, open, start, editHeader(intro, head), in.Override, func(p *policyv1.Policy) error {
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

func requireOpen(change *policyv1.PolicyChange) error {
	if change.GetState() == policyv1.PolicyChangeState_POLICY_CHANGE_STATE_OPEN {
		return nil
	}
	view := newChangeView(change)
	return invalidInputError(
		fmt.Errorf("change %s is %s: only an open change can change", view.ID, view.State),
		"List the open changes: safedep policy change list --state open")
}

func changeSyncCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "sync CHANGE",
		Short: "Move the base of a policy change to the head revision",
		Long: "Move the base of an open policy change to the head revision. The change keeps its settings " +
			"as you wrote them. The server never merges, so review the change again before you publish: " +
			"the preview shows every difference against head.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			change, err := runChangeSync(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0])
			if err != nil {
				return err
			}
			tui.Success("change %s rebased on revision %d%sreview it: safedep policy change show %s",
				change.GetChangeId(), change.GetBaseRevision(), sep, change.GetChangeId())
			return nil
		},
	}
}

func runChangeSync(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) (*policyv1.PolicyChange, error) {
	change, err := getChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if err := requireOpen(change); err != nil {
		return nil, err
	}
	req := &policyv1.SyncPolicyChangeRequest{}
	req.SetChangeId(id)
	req.SetEtag(change.GetEtag())
	res, err := client.SyncPolicyChange(ctx, req)
	if err != nil {
		return nil, mapChangeError("sync", id, err)
	}
	return res.GetChange(), nil
}

func changeDeleteCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "delete CHANGE",
		Short: "Delete an open policy change",
		Long:  "Delete an open policy change. The policy does not change. A published change stays in the history.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			if err := runChangeDelete(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0]); err != nil {
				return err
			}
			tui.Success("change %s deleted", args[0])
			return nil
		},
	}
}

func runChangeDelete(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) error {
	req := &policyv1.DeletePolicyChangeRequest{}
	req.SetChangeId(id)
	if _, err := client.DeletePolicyChange(ctx, req); err != nil {
		return mapChangeError("delete", id, err)
	}
	return nil
}

func changePublishCmd(a *app.App) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "publish CHANGE",
		Short: "Publish an open policy change as a new policy revision",
		Long: "Publish an open policy change. The publish makes a new policy revision for the whole " +
			"tenant. Projects resolve it at their next check, and endpoints apply it after their next " +
			"policy fetch. If a part that the change edits moved since its base revision, the publish " +
			"stops. Then sync the change and review it again.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			rev, err := runChangePublish(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0], message)
			if err != nil {
				return err
			}
			reportPublish(rev.GetRevision())
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "the publish message; the change title becomes the title when it is empty")
	return cmd
}

func runChangePublish(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	id, message string,
) (*policyv1.PolicyRevision, error) {
	change, err := getChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if err := requireOpen(change); err != nil {
		return nil, err
	}
	req := &policyv1.PublishPolicyChangeRequest{}
	req.SetChangeId(id)
	req.SetEtag(change.GetEtag())
	req.SetMessage(message)
	res, err := client.PublishPolicyChange(ctx, req)
	if err != nil {
		return nil, mapChangeError("publish", id, err)
	}
	return res.GetRevision(), nil
}

// reportPublish tells the admin when the new revision applies and how to
// find the endpoints that lag.
func reportPublish(revision uint64) {
	tui.Success("revision %d published", revision)
	tui.Info("  Projects resolve it at their next check. Endpoints apply it after their next policy fetch")
	tui.Info("  Endpoints that have not applied it yet: safedep endpoint list --applied-revision '<%d'", revision)
}
