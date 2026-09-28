package bitbucket

import (
	"context"
	"fmt"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/bitbucketlink"
)

// The Forge app stays installed after an unlink and keeps invoking SafeDep,
// which ignores an unlinked workspace. Only a workspace admin can uninstall
// the app, so the operator must hear both facts from the command.
const linkDeleteFollowUp = "The SafeDep Forge app stays installed in the workspace and keeps sending " +
	"events that SafeDep ignores. Uninstall the app in Bitbucket to stop that traffic. " +
	"To link again, run `safedep integration bitbucket link create` and redeem the code."

type linkDeleteInput struct {
	LinkID string
}

type linkDeleteResult struct {
	linkID    string
	workspace string
}

func linkDeleteCmd(a *app.App) *cobra.Command {
	var in linkDeleteInput
	cmd := &cobra.Command{
		Use:   "delete",
		Args:  cobra.NoArgs,
		Short: "Unlink a Bitbucket workspace from the tenant",
		Long: "Unlink a Bitbucket workspace from the active tenant. SafeDep revokes the link " +
			"and discards the stored app token. The command is idempotent: a link already " +
			"revoked succeeds. The SafeDep Forge app stays installed in the workspace until a " +
			"workspace admin uninstalls it in Bitbucket.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			integration := newIntegrationClient(client.Connection())
			result, err := runLinkDelete(cmd.Context(), integration, integration, in)
			if err != nil {
				return err
			}
			tui.Success("%s", result.message())
			tui.Info("%s", linkDeleteFollowUp)
			return nil
		},
	}
	cmd.Flags().StringVar(&in.LinkID, "link-id", "",
		"Bitbucket workspace link to delete; resolved automatically when the tenant has exactly one link")
	return cmd
}

func runLinkDelete(
	ctx context.Context,
	links bitbucketlink.Lister,
	deleter linkDeleter,
	in linkDeleteInput,
) (*linkDeleteResult, error) {
	const label = "integration bitbucket link delete"

	result := &linkDeleteResult{linkID: in.LinkID}
	if result.linkID == "" {
		listed, err := bitbucketlink.List(ctx, links, label)
		if err != nil {
			return nil, err
		}
		linkID, err := bitbucketlink.PickSingle(listed, label)
		if err != nil {
			return nil, err
		}
		result.linkID = linkID
		result.workspace = workspaceLabel(listed[0])
	}

	req := &controltowerv1.DeleteBitbucketWorkspaceLinkRequest{}
	req.SetLinkId(result.linkID)
	if _, err := deleter.DeleteBitbucketWorkspaceLink(ctx, req); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return result, nil
}

func (r *linkDeleteResult) message() string {
	if r.workspace == "" {
		return fmt.Sprintf("Unlinked Bitbucket workspace link %s", r.linkID)
	}
	return fmt.Sprintf("Unlinked Bitbucket workspace %s (link %s)", r.workspace, r.linkID)
}

func workspaceLabel(link bitbucketlink.Link) string {
	if link.WorkspaceSlug != "" {
		return link.WorkspaceSlug
	}
	return link.WorkspaceUUID
}
