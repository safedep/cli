package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const outcomePrefix = "WORKLOAD_EXCHANGE_OUTCOME_"

type exchangeListInput struct {
	Denied    bool
	Since     time.Duration
	Rule      string
	PageSize  uint32
	PageToken string
}

type exchangeListResult struct {
	Exchanges     []exchangeView `json:"exchanges"`
	NextPageToken string         `json:"next_page_token,omitempty"`
}

func exchangeListCmd(a *app.App) *cobra.Command {
	var in exchangeListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workload exchanges, newest first",
		Long: "List the workload exchanges of the tenant, newest first: each time a CI job asked for a " +
			"credential, the workload, the rule that matched, and the state of the credential. A denied " +
			"exchange says which rule is missing. --since takes a duration before now, as in 7d or 12h. " +
			"--rule takes a rule id or the name of a live rule. The server keeps an exchange for 30 days. " +
			"The command reads one page. Pass --page-token to read the next page.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			res, err := runExchangeList(cmd.Context(), client, in, time.Now())
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&in.Denied, "denied", false, "only the exchanges that the server denied")
	f.Var(duration.NewValue(&in.Since, 0), "since", "only exchanges in this time before now, as in 7d or 12h")
	f.StringVar(&in.Rule, "rule", "", "only exchanges that this rule matched, by rule id or name")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size, or the server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func runExchangeList(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	in exchangeListInput,
	now time.Time,
) (*exchangeListResult, error) {
	filters := &controltowerv1.ListWorkloadExchangesRequest_Filters{}
	set := false
	after, _, err := duration.Window(in.Since, 0, now, "exchanges")
	if err != nil {
		return nil, err
	}
	if after != nil {
		filters.SetSince(timestamppb.New(*after))
		set = true
	}
	if in.Denied {
		filters.SetOutcome(controltowerv1.WorkloadExchangeOutcome_WORKLOAD_EXCHANGE_OUTCOME_DENIED)
		set = true
	}
	if in.Rule != "" {
		id, err := ruleIDOf(ctx, client, "exchange list", in.Rule)
		if err != nil {
			return nil, err
		}
		filters.SetRuleId(id)
		set = true
	}

	req := &controltowerv1.ListWorkloadExchangesRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if set {
		req.SetFilters(filters)
	}
	res, err := client.ListWorkloadExchanges(ctx, req)
	if err != nil {
		return nil, mapError("exchange list", err)
	}
	out := &exchangeListResult{
		Exchanges:     make([]exchangeView, 0, len(res.GetExchanges())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, e := range res.GetExchanges() {
		out.Exchanges = append(out.Exchanges, newExchangeView(e))
	}
	return out, nil
}

func (r *exchangeListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *exchangeListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Exchanges))
	for _, e := range r.Exchanges {
		when := "-"
		if e.created != nil {
			when = humanize.Time(*e.created, now)
		}
		rows = append(rows, []string{
			when, e.Outcome, cloudpolicy.EmptyDash(e.DisplayIdentity), cloudpolicy.EmptyDash(e.RuleID), e.detail(now),
		})
	}
	t := table.New().
		Title("Workload exchanges").
		Headers("WHEN", "OUTCOME", "WORKLOAD", "RULE", "DETAIL").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No workload exchanges match. Try a wider --since window.")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), cloudpolicy.Plural(len(rows), "exchange", "exchanges"))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *exchangeListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("created_at\texchange_id\toutcome\tdisplay_identity\trule_id\tprovider_id\tgrant\tendpoint_id\t" +
		"expires_at\trevoked_at\tdenial_reason")
	for _, e := range r.Exchanges {
		b.WriteString("\n" + tui.PlainRow(e.CreatedAt, e.ID, e.Outcome, e.DisplayIdentity, e.RuleID, e.ProviderID,
			e.Grant, e.EndpointID, e.ExpiresAt, e.RevokedAt, e.DenialReason))
	}
	return b.String()
}
