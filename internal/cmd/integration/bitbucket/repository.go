package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const (
	scanScopePrefix            = "BITBUCKET_SCAN_SCOPE_"
	repositoryVisibilityPrefix = "VISIBILITY_"
	repositoryScanStatePrefix  = "SCAN_STATE_"
)

type repositoryListInput struct {
	LinkID string
}

type listedRepository struct {
	RepositoryUUID string `json:"repository_uuid"`
	FullName       string `json:"full_name"`
	Visibility     string `json:"visibility"`
	DefaultBranch  string `json:"default_branch,omitempty"`
	ScanState      string `json:"scan_state"`
}

type repositoryListResult struct {
	linkID       string
	scanScope    string
	repositories []listedRepository
}

type repositoryListResultJSON struct {
	LinkID       string             `json:"link_id"`
	ScanScope    string             `json:"scan_scope"`
	Repositories []listedRepository `json:"repositories"`
}

func repositoryListCmd(a *app.App) *cobra.Command {
	var in repositoryListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List repositories of a linked Bitbucket workspace",
		Long: "List the repositories of a linked Bitbucket workspace with their scan state " +
			"and the workspace's scan scope. The repository UUIDs feed " +
			"`integration bitbucket allowlist update`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			integration := newIntegrationClient(client.Connection())
			result, err := runRepositoryList(cmd.Context(), integration, integration, in)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}
	cmd.Flags().StringVar(&in.LinkID, "link-id", "",
		"Bitbucket workspace link to list through; resolved automatically when the tenant has exactly one link")
	return cmd
}

func runRepositoryList(
	ctx context.Context,
	links linkLister,
	repositories repositoryLister,
	in repositoryListInput,
) (*repositoryListResult, error) {
	const label = "integration bitbucket repository list"

	linkID := in.LinkID
	if linkID == "" {
		resolved, err := resolveLinkID(ctx, links, label)
		if err != nil {
			return nil, err
		}
		linkID = resolved
	}

	result := &repositoryListResult{linkID: linkID}
	err := paging.Paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		req := &controltowerv1.ListBitbucketRepositoriesRequest{}
		req.SetLinkId(linkID)
		req.SetPagination(paging.NewPaginationRequest(pageSize, pageToken))
		res, err := repositories.ListBitbucketRepositories(ctx, req)
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}

		result.scanScope = tui.EnumToken(res.GetScanScope().String(), scanScopePrefix)
		for i, item := range res.GetRepositories() {
			if item.GetRepositoryUuid() == "" {
				return "", fmt.Errorf("%s: invalid response: repository %d is missing its UUID", label, i+1)
			}
			result.repositories = append(result.repositories, listedRepository{
				RepositoryUUID: item.GetRepositoryUuid(),
				FullName:       item.GetFullName(),
				Visibility:     tui.EnumToken(item.GetVisibility().String(), repositoryVisibilityPrefix),
				DefaultBranch:  item.GetDefaultBranch(),
				ScanState:      tui.EnumToken(item.GetScanState().String(), repositoryScanStatePrefix),
			})
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *repositoryListResult) RenderJSON() ([]byte, error) {
	repositories := r.repositories
	if repositories == nil {
		repositories = []listedRepository{}
	}
	return json.MarshalIndent(repositoryListResultJSON{
		LinkID:       r.linkID,
		ScanScope:    r.scanScope,
		Repositories: repositories,
	}, "", "  ")
}

func (r *repositoryListResult) RenderPlain() string {
	var output strings.Builder
	output.WriteString("full_name\trepository_uuid\tvisibility\tdefault_branch\tscan_state")
	for _, repository := range r.repositories {
		output.WriteByte('\n')
		output.WriteString(strings.Join(repositoryCells(repository), "\t"))
	}
	return output.String()
}

func (r *repositoryListResult) RenderTable() string {
	rows := make([][]string, 0, len(r.repositories))
	for _, repository := range r.repositories {
		rows = append(rows, repositoryCells(repository))
	}
	return table.New().
		Title("Bitbucket repositories").
		Headers("FULL NAME", "REPOSITORY UUID", "VISIBILITY", "DEFAULT BRANCH", "SCAN STATE").
		Rows(rows...).
		Footer(fmt.Sprintf("%d %s in link %s, scan scope %s",
			len(rows), pluralRepositories(len(rows)), r.linkID, r.scanScope)).
		Render()
}

func repositoryCells(repository listedRepository) []string {
	return []string{
		repository.FullName,
		repository.RepositoryUUID,
		repository.Visibility,
		repository.DefaultBranch,
		repository.ScanState,
	}
}

func pluralRepositories(n int) string {
	if n == 1 {
		return "repository"
	}
	return "repositories"
}
