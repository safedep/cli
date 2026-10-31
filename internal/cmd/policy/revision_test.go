package policy

import (
	"context"
	"testing"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func TestRunRevisionList(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.ListPolicyRevisionsResponse{}
	res.SetRevisions([]*policyv1.PolicyRevision{revision(51, "delete r_80b"), revision(50, "extend r_77e")})
	client.EXPECT().ListPolicyRevisions(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPolicyRevisionsRequest) bool {
		return r.GetPagination().GetPageSize() == 5
	})).Return(res, nil)

	got, err := runRevisionList(context.Background(), client, revisionListInput{PageSize: 5})
	require.NoError(t, err)
	table := got.RenderTable()
	assert.Contains(t, table, "delete r_80b")
	assert.Contains(t, table, "CLI")
	assert.Contains(t, table, "TIGHTENS")
	assert.Contains(t, got.RenderPlain(), "51\t")
}

func TestRunRevisionShow(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.GetPolicyRevisionResponse{}
	rev := revision(47, "payments: fail on medium")
	rev.SetChangeId("18")
	res.SetRevision(rev)
	res.SetPolicy(headPolicy())
	client.EXPECT().GetPolicyRevision(mock.Anything, mock.Anything).Return(res, nil)

	got, err := runRevisionShow(context.Background(), client, 47)
	require.NoError(t, err)
	table := got.RenderTable()
	assert.Contains(t, table, "18 · safedep policy change show 18")
	assert.Contains(t, table, "Policy settings\nbase:")
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"change_id": "18"`)
}

func TestParseRevision(t *testing.T) {
	n, err := parseRevision("47")
	require.NoError(t, err)
	assert.Equal(t, uint64(47), n)
	_, err = parseRevision("0")
	assert.Error(t, err)
	_, err = parseRevision("head")
	assert.Error(t, err)
}
