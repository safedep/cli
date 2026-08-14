package subscription

import (
	"encoding/json"
	"testing"
	"time"

	msgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sampleEndpointUsage() *EndpointUsage {
	return &EndpointUsage{
		UnitsUsed:     4,
		UnitsIncluded: 5,
		HasIncluded:   true,
		Breakdown: []AssetClassUsage{
			{DisplayName: "Machines", ActiveAssets: 2, AssetsPerUnit: 1, Units: 2},
			{DisplayName: "Repositories", ActiveAssets: 4, AssetsPerUnit: 3, Units: 2},
		},
		PeriodEnd: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestRenderEndpointUsage(t *testing.T) {
	out := renderEndpointUsage(sampleEndpointUsage())
	assert.Contains(t, out, "SDLC Endpoints: this month (resets 1 Sep)")
	// Plain output mode degrades the bar to "label: value/max (pct)".
	assert.Contains(t, out, "Units: 4/5 (80%)")
	assert.Contains(t, out, "Machines")
	assert.Contains(t, out, "Repositories")
}

func TestRenderEndpointUsageNoAllotment(t *testing.T) {
	eu := sampleEndpointUsage()
	eu.HasIncluded = false
	eu.UnitsIncluded = 0

	out := renderEndpointUsage(eu)
	assert.Contains(t, out, "Units used   4 (account has no defined allotment)")
	assert.NotContains(t, out, "4 / 5")
}

func TestRenderEndpointUsageFull(t *testing.T) {
	eu := sampleEndpointUsage()
	eu.UnitsUsed = 5

	out := renderEndpointUsage(eu)
	assert.Contains(t, out, "Units: 5/5 (100%)")
}

func TestEndpointUsageFromProto(t *testing.T) {
	end := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	eu := endpointUsageFromProto(msgv1.SdlcEndpointUsage_builder{
		UnitsUsed:     4,
		UnitsIncluded: proto.Int64(5),
		Breakdown: []*msgv1.SdlcEndpointUsage_AssetClassUsage{
			msgv1.SdlcEndpointUsage_AssetClassUsage_builder{
				AssetClass:    msgv1.AssetClass_ASSET_CLASS_MACHINE,
				DisplayName:   "Machines",
				ActiveAssets:  2,
				AssetsPerUnit: 1,
				Units:         2,
			}.Build(),
		},
		PeriodEnd: timestamppb.New(end),
	}.Build())

	require.NotNil(t, eu)
	assert.EqualValues(t, 4, eu.UnitsUsed)
	assert.True(t, eu.HasIncluded)
	assert.EqualValues(t, 5, eu.UnitsIncluded)
	assert.Equal(t, end, eu.PeriodEnd)
	require.Len(t, eu.Breakdown, 1)
	assert.Equal(t, "Machines", eu.Breakdown[0].DisplayName)
	assert.EqualValues(t, 2, eu.Breakdown[0].ActiveAssets)
}

func TestEndpointUsageFromProtoAbsentAllotment(t *testing.T) {
	eu := endpointUsageFromProto(msgv1.SdlcEndpointUsage_builder{UnitsUsed: 7}.Build())
	require.NotNil(t, eu)
	assert.False(t, eu.HasIncluded)
	assert.Zero(t, eu.UnitsIncluded)
	assert.True(t, eu.PeriodEnd.IsZero())
}

func TestStatusRenderJSONIncludesEndpointUsage(t *testing.T) {
	r := &statusResult{acct: &AccountStatus{Status: statusFree, Endpoints: sampleEndpointUsage()}}
	b, err := r.RenderJSON()
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	eu, ok := m["endpoint_usage"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 4, eu["units_used"])
	assert.EqualValues(t, 5, eu["units_included"])
	assert.Equal(t, "2026-09-01", eu["resets_at"])

	breakdown, ok := eu["breakdown"].([]any)
	require.True(t, ok)
	require.Len(t, breakdown, 2)
	first := breakdown[0].(map[string]any)
	assert.Equal(t, "Machines", first["class"])
	assert.EqualValues(t, 1, first["assets_per_unit"])
}

func TestStatusRenderJSONOmitsIncludedWhenAbsent(t *testing.T) {
	eu := sampleEndpointUsage()
	eu.HasIncluded = false
	r := &statusResult{acct: &AccountStatus{Status: statusActive, Endpoints: eu}}
	b, err := r.RenderJSON()
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	entry := m["endpoint_usage"].(map[string]any)
	_, present := entry["units_included"]
	assert.False(t, present)
}

func TestStatusRenderPlainIncludesEndpointUsage(t *testing.T) {
	r := &statusResult{acct: &AccountStatus{Status: statusFree, Endpoints: sampleEndpointUsage()}}
	out := r.RenderPlain()
	assert.Contains(t, out, "endpoint_units\t4 / 5")
	assert.Contains(t, out, "endpoint_class\tMachines active=2 per_unit=1 units=2")
	assert.Contains(t, out, "endpoint_class\tRepositories active=4 per_unit=3 units=2")
}

func TestStatusRenderTableIncludesEndpointUsage(t *testing.T) {
	r := &statusResult{acct: &AccountStatus{Status: statusFree, Endpoints: sampleEndpointUsage()}}
	out := r.RenderTable()
	assert.Contains(t, out, "SDLC Endpoints")
	assert.Contains(t, out, "Machines")
	assert.Contains(t, out, "Repositories")
}
