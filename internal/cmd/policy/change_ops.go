package policy

import (
	"context"
	"fmt"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

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
			id := clitui.EscapeLine(change.GetChangeId())
			tui.Success("change %s rebased on revision %d%sreview it: safedep policy change show %s",
				id, change.GetBaseRevision(), sep, id)
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
			tui.Success("change %s deleted", clitui.EscapeLine(args[0]))
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
