package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/paging"
	clitui "github.com/safedep/cli/internal/tui"
)

type revisionListInput struct {
	PageSize  uint32
	PageToken string
}

type revisionListResult struct {
	Revisions     []*revisionView `json:"revisions"`
	NextPageToken string          `json:"next_page_token,omitempty"`
}

func revisionListCmd(a *app.App) *cobra.Command {
	var in revisionListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List policy revisions, newest first",
		Long: "List the policy revisions of the tenant, newest first, with the author, the source, the change " +
			"direction and the publish message. The command reads one page. Pass --page-token to " +
			"read the next page.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runRevisionList(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().Uint32Var(&in.PageSize, "limit", 0, "page size, or the server default when 0")
	cmd.Flags().StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func runRevisionList(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	in revisionListInput,
) (*revisionListResult, error) {
	req := &policyv1.ListPolicyRevisionsRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	res, err := client.ListPolicyRevisions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("policy revision list: %w", err)
	}
	out := &revisionListResult{
		Revisions:     make([]*revisionView, 0, len(res.GetRevisions())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, r := range res.GetRevisions() {
		out.Revisions = append(out.Revisions, newRevisionView(r))
	}
	return out, nil
}

func (r *revisionListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *revisionListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Revisions))
	for _, rev := range r.Revisions {
		rows = append(rows, []string{
			fmt.Sprintf("%d", rev.Revision), humanTimeCell(rev.PublishedAt, now), cloudpolicy.EmptyDash(rev.Author),
			rev.Source, rev.Direction, cloudpolicy.EmptyDash(rev.Message),
		})
	}
	t := table.New().
		Title("Policy revisions").
		Headers("REV", "PUBLISHED", "BY", "SOURCE", "DIRECTION", "MESSAGE").
		Rows(clitui.EscapeCells(rows)...).
		EmptyMessage("No policy revisions. The tenant has not published a policy yet.")
	if r.NextPageToken != "" {
		t = t.Footer("More available: --page-token " + r.NextPageToken)
	}
	return t.Render()
}

func (r *revisionListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("revision\tpublished_at\tauthor\tsource\tdirection\tchange_id\tmessage")
	for _, rev := range r.Revisions {
		b.WriteString("\n" + clitui.PlainRow(fmt.Sprint(rev.Revision), rev.Published, rev.Author, rev.Source,
			rev.Direction, rev.ChangeID, rev.Message))
	}
	return b.String()
}
