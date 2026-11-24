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

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const (
	providerKindPrefix = "WORKLOAD_IDENTITY_PROVIDER_KIND_"
	linkStatePrefix    = "WORKLOAD_IDENTITY_PROVIDER_LINK_STATE_"
	noProvidersHelp    = "Link a GitHub organization with the SafeDep GitHub App in SafeDep Cloud. " +
		"A verified link creates the provider of its account"
)

type providerView struct {
	ID           string `json:"provider_id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	AccountLogin string `json:"account_login"`
	OwnerID      string `json:"owner_id"`
	LinkState    string `json:"link_state"`
	LinkID       string `json:"link_id"`
	CreatedAt    string `json:"created_at,omitempty"`
	created      *time.Time
}

func newProviderView(p *controltowerv1.WorkloadIdentityProvider) providerView {
	v := providerView{
		ID:           p.GetProviderId(),
		Name:         providerName(p),
		Kind:         tui.EnumToken(p.GetKind().String(), providerKindPrefix),
		AccountLogin: p.GetAccountLogin(),
		OwnerID:      p.GetOwnerId(),
		LinkState:    tui.EnumToken(p.GetLinkState().String(), linkStatePrefix),
		LinkID:       p.GetLinkId(),
		created:      cloudpolicy.TimeOf(p.GetCreatedAt()),
	}
	if v.created != nil {
		v.CreatedAt = v.created.Format(time.RFC3339)
	}
	return v
}

// providerName is the KIND:ACCOUNT form that --provider takes, as in
// github-actions:acme.
func providerName(p *controltowerv1.WorkloadIdentityProvider) string {
	return tui.EnumToken(p.GetKind().String(), providerKindPrefix) + ":" + p.GetAccountLogin()
}

type providerListInput struct {
	PageSize  uint32
	PageToken string
}

type providerListResult struct {
	Providers     []providerView `json:"providers"`
	NextPageToken string         `json:"next_page_token,omitempty"`
}

func providerListCmd(a *app.App) *cobra.Command {
	var in providerListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the workload identity providers of the tenant",
		Long: "List the workload identity providers of the tenant, with the KIND:ACCOUNT name that " +
			"`safedep workload rule create --provider` takes, and the state of the link that each provider " +
			"trusts. A CI job gets a credential only through a provider whose link is active. The command " +
			"reads one page. Pass --page-token to read the next page.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := adminClient(a)
			if err != nil {
				return err
			}
			res, err := runProviderList(cmd.Context(), client, in)
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

func runProviderList(
	ctx context.Context,
	client controltowerv1grpc.WorkloadAdminServiceClient,
	in providerListInput,
) (*providerListResult, error) {
	req := &controltowerv1.ListWorkloadIdentityProvidersRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	res, err := client.ListWorkloadIdentityProviders(ctx, req)
	if err != nil {
		return nil, mapError("provider list", err)
	}
	out := &providerListResult{
		Providers:     make([]providerView, 0, len(res.GetProviders())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, p := range res.GetProviders() {
		out.Providers = append(out.Providers, newProviderView(p))
	}
	return out, nil
}

func (r *providerListResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *providerListResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Providers))
	for _, p := range r.Providers {
		created := "-"
		if p.created != nil {
			created = humanize.Time(*p.created, now)
		}
		rows = append(rows, []string{p.Name, p.ID, p.OwnerID, p.LinkState, created})
	}
	t := table.New().
		Title("Workload identity providers").
		Headers("PROVIDER", "ID", "OWNER ID", "LINK", "CREATED").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No workload identity providers. " + noProvidersHelp + ".")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), cloudpolicy.Plural(len(rows), "provider", "providers"))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *providerListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("provider_id\tname\tkind\taccount_login\towner_id\tlink_state\tlink_id\tcreated_at")
	for _, p := range r.Providers {
		b.WriteString("\n" + tui.PlainRow(p.ID, p.Name, p.Kind, p.AccountLogin, p.OwnerID, p.LinkState, p.LinkID, p.CreatedAt))
	}
	return b.String()
}
