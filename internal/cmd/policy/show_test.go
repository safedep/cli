package policy

import (
	"context"
	"encoding/json"
	"testing"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func TestRunShow(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	res, err := runShow(context.Background(), client, "platform-eng.acme.safedep.io")
	require.NoError(t, err)

	require.Len(t, res.Overrides, 2)
	assert.Equal(t, "ci", res.Overrides[0].Name, "the highest priority comes first")
	assert.Equal(t, "managed/deployment-kind=ci", res.Overrides[0].Selector)

	table := res.RenderTable()
	for _, want := range []string{
		"Policy          platform-eng.acme.safedep.io",
		`46 · published 2d ago by alice · "cooldown 5d for laptops"`,
		"Open changes    none",
		"211 (184 trust, 27 block) · 23 expire in 7d",
		"revision 12 · scan_gate.fail_on: high",
		"Overrides (priority, highest first)",
		"payments",
	} {
		assert.Contains(t, table, want)
	}

	b, err := res.RenderJSON()
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	assert.InDelta(t, 46, out["head_revision"], 0)
	assert.Equal(t, map[string]any{"scan_gate.fail_on": "high"}, out["default_policy"])
	policy, ok := out["policy"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, policy, "overrides")

	assert.Contains(t, res.RenderPlain(), "override\t300\tci\tmanaged/deployment-kind=ci")
}

func TestRunShow_NeverPublished(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.GetPolicyResponse{}
	res.SetOpenChangeCount(2)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(res, nil)

	got, err := runShow(context.Background(), client, "")
	require.NoError(t, err)
	table := got.RenderTable()
	assert.Contains(t, table, "none: the tenant has not published a policy yet")
	assert.Contains(t, table, "2 · safedep policy change list --state open")
	assert.Contains(t, table, "No policy overrides")
}

func TestRunGet(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	res, err := runGet(context.Background(), client, getInput{}, "acme")
	require.NoError(t, err)
	assert.Equal(t, uint64(46), res.Revision)
	assert.Contains(t, res.RenderPlain(), "# Policy settings of acme · revision 46\n")
	assert.Contains(t, res.RenderTable(), "overrides:\n  - name: payments")
	b, err := res.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"revision": 46`)
}

func TestRunGet_AtRevision(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	rev := &policyv1.GetPolicyRevisionResponse{}
	rev.SetRevision(revision(40, "old"))
	rev.SetPolicy(headPolicy())
	client.EXPECT().GetPolicyRevision(mock.Anything, mock.MatchedBy(func(r *policyv1.GetPolicyRevisionRequest) bool {
		return r.GetRevision() == 40
	})).Return(rev, nil)

	res, err := runGet(context.Background(), client, getInput{Revision: 40}, "")
	require.NoError(t, err)
	assert.Contains(t, res.YAML, "# Policy settings of this tenant · revision 40")
}
