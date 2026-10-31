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
	"github.com/safedep/cli/internal/tui"
)

type changeListInput struct {
	State     string
	PageSize  uint32
	PageToken string
}

type changeListResult struct {
	Changes       []changeView `json:"changes"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

func changeListCmd(a *app.App) *cobra.Command {
	var in changeListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List policy changes, newest first",
		Long: "List the policy changes of the tenant, newest first. Filter by state. The command reads one page. " +
			"Pass --page-token to read the next page.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			_, err := parseChangeState(in.State)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runChangeList(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.State, "state", "", "filter: change state ("+strings.Join(changeStateTokens(), ", ")+")")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func changeStateTokens() []string {
	return tui.EnumTokens(policyv1.PolicyChangeState_name, changeStatePrefix)
}

func parseChangeState(token string) (policyv1.PolicyChangeState, error) {
	if token == "" {
		return policyv1.PolicyChangeState_POLICY_CHANGE_STATE_UNSPECIFIED, nil
	}
	n, ok := tui.ParseEnumToken(policyv1.PolicyChangeState_name, changeStatePrefix, token)
	if !ok {
		return 0, invalidInputError(
			fmt.Errorf("unknown --state value %q: allowed values are %s", token, strings.Join(changeStateTokens(), ", ")),
			"Retry with one of: "+strings.Join(changeStateTokens(), ", "))
	}
	return policyv1.PolicyChangeState(n), nil
}

func runChangeList(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	in changeListInput,
) (*changeListResult, error) {
	state, err := parseChangeState(in.State)
	if err != nil {
		return nil, err
	}
	req := &policyv1.ListPolicyChangesRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if state != policyv1.PolicyChangeState_POLICY_CHANGE_STATE_UNSPECIFIED {
		filters := &policyv1.ListPolicyChangesRequest_Filters{}
		filters.SetState(state)
		req.SetFilters(filters)
	}
	res, err := client.ListPolicyChanges(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("policy change list: %w", err)
	}
	out := &changeListResult{
		Changes:       make([]changeView, 0, len(res.GetChanges())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, c := range res.GetChanges() {
		out.Changes = append(out.Changes, newChangeView(c))
	}
	return out, nil
}

func (r *changeListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *changeListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Changes))
	for _, c := range r.Changes {
		state := c.State
		if c.PublishedRevision > 0 {
			state = fmt.Sprintf("%s as rev %d", state, c.PublishedRevision)
		}
		rows = append(rows, []string{
			c.ID, c.titleOrUntitled(), state, fmt.Sprintf("%d", c.BaseRevision),
			c.contents(), cloudpolicy.EmptyDash(c.Author), humanTimeCell(c.updatedAt, now),
		})
	}
	t := table.New().
		Title("Policy changes").
		Headers("CHANGE", "TITLE", "STATE", "BASE REV", "EDITS", "AUTHOR", "UPDATED").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No policy changes. Create one with: safedep policy edit")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), cloudpolicy.Plural(len(rows), "change", "changes"))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *changeListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("change_id\ttitle\tstate\tbase_revision\tpublished_revision\tauthor\tupdated_at")
	for _, c := range r.Changes {
		b.WriteString("\n" + tui.PlainRow(c.ID, c.Title, c.State, fmt.Sprint(c.BaseRevision),
			fmt.Sprint(c.PublishedRevision), c.Author, c.Updated))
	}
	return b.String()
}
