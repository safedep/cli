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
	Account string
	LinkID  string
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
			"and discards the stored app token. --account names the workspace by its slug. Without it, " +
			"the command needs exactly one active link. A repeat delete fails with not found, because " +
			"the link is no longer active. The SafeDep Forge app stays installed in the workspace until a " +
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
	addLinkFlags(cmd, &in.Account, &in.LinkID, "to delete")
	return cmd
}

func runLinkDelete(
	ctx context.Context,
	links bitbucketlink.Lister,
	deleter linkDeleter,
	in linkDeleteInput,
) (*linkDeleteResult, error) {
	const label = "integration bitbucket link delete"

	link, err := bitbucketlink.Resolve(ctx, links, in.Account, in.LinkID, label)
	if err != nil {
		return nil, err
	}
	result := &linkDeleteResult{linkID: link.ID, workspace: workspaceLabel(link)}

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

// workspaceLabel prefers the slug and falls back to the immutable UUID,
// because the server leaves the slug empty until its first sync. A link that
// only --link-id named carries neither, and the message names the link.
func workspaceLabel(link bitbucketlink.Link) string {
	if link.WorkspaceSlug != "" {
		return link.WorkspaceSlug
	}
	return link.WorkspaceUUID
}
