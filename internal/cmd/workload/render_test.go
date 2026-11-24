package workload

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/tui"
)

func TestListResults_Render(t *testing.T) {
	created := timestamppb.New(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	provider := activeProvider("prov_acme", "acme")
	provider.SetCreatedAt(created)
	rule := trustRule("r_web", "web", map[string]string{"acme/web": "123456"}, condition(claimRef, equals, "refs/heads/main"))
	exchange := controltowerv1.WorkloadExchange_builder{
		ExchangeId:   "x1",
		Outcome:      controltowerv1.WorkloadExchangeOutcome_WORKLOAD_EXCHANGE_OUTCOME_DENIED,
		DenialReason: "no workload trust rule gives TOOL to acme/web on refs/heads/x",
		ClaimSummary: controltowerv1.WorkloadClaimSummary_builder{DisplayIdentity: "github:acme/web\x1b[2J run 1/1"}.Build(),
		CreatedAt:    created,
	}.Build()
	names := map[string]string{"prov_acme": "github-actions:acme"}

	tests := []struct {
		name      string
		result    tui.Renderable
		itemsKey  string
		wantTable []string
		wantPlain []string
	}{
		{
			name:      "providers",
			result:    &providerListResult{Providers: []providerView{newProviderView(provider)}, NextPageToken: "tok"},
			itemsKey:  "providers",
			wantTable: []string{"Workload identity providers", "github-actions:acme", "prov_acme", "active", "More available: --page-token tok"},
			wantPlain: []string{
				"provider_id\tname\tkind\taccount_login\towner_id\tlink_state\tlink_id\tcreated_at",
				"prov_acme\tgithub-actions:acme\tgithub-actions\tacme\t1001\tactive\tlink-prov_acme\t2026-11-20T09:00:00Z",
			},
		},
		{
			name:      "rules",
			result:    &ruleListResult{Rules: []ruleView{newRuleView(rule, names)}, NextPageToken: "tok"},
			itemsKey:  "rules",
			wantTable: []string{"Workload trust rules", "r_web", "acme/web", "ref = refs/heads/main", "never", "More available: --page-token tok"},
			wantPlain: []string{
				"rule_id\tname\tprovider\tprovider_id\tgrant\trepositories\tconditions\tproject_binding\tlast_used_at",
				"r_web\tweb\tgithub-actions:acme\tprov_acme\ttool\tacme/web=123456\tref = refs/heads/main\tmanual\t",
			},
		},
		{
			name:      "exchanges",
			result:    &exchangeListResult{Exchanges: []exchangeView{newExchangeView(exchange)}, NextPageToken: "tok"},
			itemsKey:  "exchanges",
			wantTable: []string{"Workload exchanges", "denied", `github:acme/web\x1b[2J run 1/1`, "no workload trust rule gives TOOL", "More available: --page-token tok"},
			wantPlain: []string{
				"created_at\texchange_id\toutcome\tdisplay_identity\trule_id\tprovider_id\tgrant\tendpoint_id\texpires_at\trevoked_at\tdenial_reason",
				"2026-11-20T09:00:00Z\tx1\tdenied\tgithub:acme/web\\x1b[2J run 1/1\t\t\tunknown\t\t\t\tno workload trust rule gives TOOL to acme/web on refs/heads/x",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tt.result.RenderJSON()
			require.NoError(t, err)
			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(out, &decoded))
			var items []json.RawMessage
			require.NoError(t, json.Unmarshal(decoded[tt.itemsKey], &items))
			assert.Len(t, items, 1)
			assert.JSONEq(t, `"tok"`, string(decoded["next_page_token"]))

			table := tt.result.RenderTable()
			for _, w := range tt.wantTable {
				assert.Contains(t, table, w)
			}
			assert.NotContains(t, table, "\x1b[2J")

			assert.Equal(t, tt.wantPlain, strings.Split(tt.result.RenderPlain(), "\n"))
		})
	}
}
