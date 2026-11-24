package workload

import (
	"context"
	"testing"
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/cmd/workload/mocks"
)

func TestRunExchangeList_Filters(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		in    exchangeListInput
		rules *controltowerv1.ListWorkloadTrustRulesResponse
		check func(t *testing.T, f *controltowerv1.ListWorkloadExchangesRequest_Filters, hasFilters bool)
	}{
		{
			name: "no filter",
			check: func(t *testing.T, _ *controltowerv1.ListWorkloadExchangesRequest_Filters, hasFilters bool) {
				assert.False(t, hasFilters)
			},
		},
		{
			name: "denied since 7d",
			in:   exchangeListInput{Denied: true, Since: 7 * 24 * time.Hour},
			check: func(t *testing.T, f *controltowerv1.ListWorkloadExchangesRequest_Filters, _ bool) {
				assert.Equal(t, controltowerv1.WorkloadExchangeOutcome_WORKLOAD_EXCHANGE_OUTCOME_DENIED, f.GetOutcome())
				assert.Equal(t, now.Add(-7*24*time.Hour), f.GetSince().AsTime())
				assert.False(t, f.HasRuleId())
			},
		},
		{
			name:  "a rule by name",
			in:    exchangeListInput{Rule: "org"},
			rules: rulesResponse(trustRule("r_web", "web", nil), trustRule("r_org", "org", nil)),
			check: func(t *testing.T, f *controltowerv1.ListWorkloadExchangesRequest_Filters, _ bool) {
				assert.Equal(t, "r_org", f.GetRuleId())
				assert.False(t, f.HasOutcome())
				assert.False(t, f.HasSince())
			},
		},
		{
			name:  "a deleted rule by id",
			in:    exchangeListInput{Rule: "r_gone"},
			rules: rulesResponse(trustRule("r_org", "org", nil)),
			check: func(t *testing.T, f *controltowerv1.ListWorkloadExchangesRequest_Filters, _ bool) {
				assert.Equal(t, "r_gone", f.GetRuleId())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewWorkloadAdminServiceClientMock(t)
			if tt.rules != nil {
				client.EXPECT().ListWorkloadTrustRules(mock.Anything, mock.Anything).Return(tt.rules, nil)
			}
			var sent *controltowerv1.ListWorkloadExchangesRequest
			client.EXPECT().ListWorkloadExchanges(mock.Anything, mock.MatchedBy(func(r *controltowerv1.ListWorkloadExchangesRequest) bool {
				sent = r
				return true
			})).Return(&controltowerv1.ListWorkloadExchangesResponse{}, nil)

			_, err := runExchangeList(context.Background(), client, tt.in, now)

			require.NoError(t, err)
			require.NotNil(t, sent)
			tt.check(t, sent.GetFilters(), sent.HasFilters())
		})
	}
}

func TestExchangeView_Detail(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time {
		t := now.Add(d)
		return &t
	}
	tests := []struct {
		name string
		view exchangeView
		want string
	}{
		{name: "denied", view: exchangeView{DenialReason: "no workload trust rule gives TOOL to acme/web on refs/heads/x"},
			want: "no workload trust rule gives TOOL to acme/web on refs/heads/x"},
		{name: "revoked before its expiry", view: exchangeView{expires: at(time.Hour), revoked: at(-2 * time.Minute)}, want: "revoked 2m ago"},
		{name: "live", view: exchangeView{expires: at(5 * time.Minute)}, want: "expires in 5m"},
		{name: "expired", view: exchangeView{expires: at(-3 * time.Hour)}, want: "expired 3h ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.view.detail(now))
		})
	}
}

func TestRunExchangeList_RuleThatNamesNoLiveRule(t *testing.T) {
	issued := controltowerv1.WorkloadExchange_builder{
		ExchangeId: "x1",
		Outcome:    controltowerv1.WorkloadExchangeOutcome_WORKLOAD_EXCHANGE_OUTCOME_ISSUED,
		RuleId:     "r_gone",
	}.Build()
	tests := []struct {
		name       string
		rule       string
		exchanges  []*controltowerv1.WorkloadExchange
		wantNotice string
		wantEmpty  string
	}{
		{
			name:       "a name of no live rule and no exchange",
			rule:       "web-old",
			wantNotice: "No live rule has the name or id web-old. To list the exchanges of a deleted rule, pass its rule id",
			wantEmpty:  "No workload exchanges match.",
		},
		{
			name:      "the id of a deleted rule with exchanges",
			rule:      "r_gone",
			exchanges: []*controltowerv1.WorkloadExchange{issued},
		},
		{
			name:      "a live rule with no exchange",
			rule:      "org",
			wantEmpty: "No workload exchanges match. Try a wider --since window.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewWorkloadAdminServiceClientMock(t)
			expectRulePages(client, []*controltowerv1.WorkloadTrustRule{trustRule("r_org", "org", nil)})
			client.EXPECT().ListWorkloadExchanges(mock.Anything, mock.Anything).
				Return(controltowerv1.ListWorkloadExchangesResponse_builder{Exchanges: tt.exchanges}.Build(), nil)

			res, err := runExchangeList(context.Background(), client, exchangeListInput{Rule: tt.rule}, time.Now())

			require.NoError(t, err)
			assert.Equal(t, tt.wantNotice, res.notice())
			if tt.wantEmpty != "" {
				assert.Contains(t, res.RenderTable(), tt.wantEmpty)
			}
			if tt.wantNotice != "" {
				// A wider window cannot help a rule that the server does not know.
				assert.NotContains(t, res.RenderTable(), "--since")
			}
		})
	}
}
