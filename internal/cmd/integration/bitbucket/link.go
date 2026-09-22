package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/bitbucketlink"
)

type linkCreateResult struct {
	linkCode  string
	expiresAt *time.Time
}

type linkCreateResultJSON struct {
	LinkCode  string `json:"link_code"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func linkCreateCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Args:  cobra.NoArgs,
		Short: "Issue a link code for a Bitbucket workspace",
		Long: "Issue the short-lived, single-use code a Bitbucket workspace admin pastes into " +
			"the workspace's SafeDep settings page (added by the SafeDep Forge app) to link the " +
			"workspace to the active tenant. The code is shown once. Issuing a new code " +
			"invalidates every earlier unredeemed code of the tenant.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			result, err := runLinkCreate(cmd.Context(), newIntegrationClient(client.Connection()))
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
}

func runLinkCreate(ctx context.Context, client linkCodeCreator) (*linkCreateResult, error) {
	const label = "integration bitbucket link create"

	res, err := client.CreateBitbucketWorkspaceLinkCode(
		ctx,
		&controltowerv1.CreateBitbucketWorkspaceLinkCodeRequest{},
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if res.GetLinkCode() == "" {
		return nil, fmt.Errorf("%s: invalid response: missing link code", label)
	}

	result := &linkCreateResult{linkCode: res.GetLinkCode()}
	if timestamp := res.GetExpiresAt(); timestamp != nil {
		expiresAt := timestamp.AsTime().UTC()
		result.expiresAt = &expiresAt
	}
	return result, nil
}

func (r *linkCreateResult) RenderJSON() ([]byte, error) {
	item := linkCreateResultJSON{LinkCode: r.linkCode}
	if r.expiresAt != nil {
		item.ExpiresAt = r.expiresAt.Format(time.RFC3339)
	}
	return json.MarshalIndent(item, "", "  ")
}

func (r *linkCreateResult) RenderPlain() string {
	expiresAt := ""
	if r.expiresAt != nil {
		expiresAt = r.expiresAt.Format(time.RFC3339)
	}
	return "link_code\texpires_at\n" + r.linkCode + "\t" + expiresAt
}

func (r *linkCreateResult) RenderTable() string {
	// The code lives for minutes, so the table shows the remaining lifetime
	// via the shared humanizer, like auth status and the list commands. The
	// plain and JSON output keep the exact instant.
	expires := ""
	if r.expiresAt != nil {
		expires = humanize.Time(*r.expiresAt, time.Now())
	}
	return table.New().
		Title("Bitbucket workspace link code").
		Headers("LINK CODE", "EXPIRES").
		Rows([]string{r.linkCode, expires}).
		Footer("Paste the code into the workspace's SafeDep settings page in Bitbucket before it expires.").
		Render()
}

type linkListResult struct {
	links []bitbucketlink.Link
}

type workspaceLinkJSON struct {
	LinkID        string `json:"link_id"`
	WorkspaceUUID string `json:"workspace_uuid"`
	WorkspaceSlug string `json:"workspace_slug,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}

type linkListResultJSON struct {
	Links []workspaceLinkJSON `json:"links"`
}

func linkListCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List Bitbucket workspaces linked to the tenant",
		Long: "List every Bitbucket workspace linked to the active tenant, with the link ID " +
			"the repository and allowlist commands take.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			result, err := runLinkList(cmd.Context(), newIntegrationClient(client.Connection()))
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
}

func runLinkList(ctx context.Context, client bitbucketlink.Lister) (*linkListResult, error) {
	links, err := bitbucketlink.List(ctx, client, "integration bitbucket link list")
	if err != nil {
		return nil, err
	}
	return &linkListResult{links: links}, nil
}

func (r *linkListResult) RenderJSON() ([]byte, error) {
	links := make([]workspaceLinkJSON, 0, len(r.links))
	for _, link := range r.links {
		links = append(links, workspaceLinkJSON{
			LinkID:        link.ID,
			WorkspaceUUID: link.WorkspaceUUID,
			WorkspaceSlug: link.WorkspaceSlug,
			WorkspaceName: link.WorkspaceName,
		})
	}
	return json.MarshalIndent(linkListResultJSON{Links: links}, "", "  ")
}

func (r *linkListResult) RenderPlain() string {
	var output strings.Builder
	output.WriteString("link_id\tworkspace_uuid\tworkspace_slug\tworkspace_name")
	for _, link := range r.links {
		output.WriteByte('\n')
		output.WriteString(strings.Join(workspaceLinkCells(link), "\t"))
	}
	return output.String()
}

func (r *linkListResult) RenderTable() string {
	rows := make([][]string, 0, len(r.links))
	for _, link := range r.links {
		rows = append(rows, workspaceLinkCells(link))
	}
	return table.New().
		Title("Linked Bitbucket workspaces").
		Headers("LINK ID", "WORKSPACE UUID", "SLUG", "NAME").
		Rows(rows...).
		Footer(fmt.Sprintf("%d %s", len(rows), pluralWorkspaces(len(rows)))).
		Render()
}

func workspaceLinkCells(link bitbucketlink.Link) []string {
	return []string{link.ID, link.WorkspaceUUID, link.WorkspaceSlug, link.WorkspaceName}
}

func pluralWorkspaces(n int) string {
	if n == 1 {
		return "workspace"
	}
	return "workspaces"
}
