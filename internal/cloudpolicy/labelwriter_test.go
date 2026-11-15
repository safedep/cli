package cloudpolicy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func TestSetLabels(t *testing.T) {
	client := mocks.NewLabelServiceClientMock(t)
	labels, err := ParseCustomLabels([]string{"custom/criticality=high"})
	require.NoError(t, err)

	client.EXPECT().SetSubjectLabels(mock.Anything, mock.MatchedBy(func(req *policyv1.SetSubjectLabelsRequest) bool {
		return len(req.GetSubjects()) == 2 &&
			req.GetSubjects()[0].GetKind() == specv1.SubjectKind_SUBJECT_KIND_PROJECT &&
			req.GetSubjects()[0].GetId() == "p_1" &&
			req.GetSubjects()[1].GetId() == "p_2" &&
			req.GetLabels()[0].GetKey() == "custom/criticality"
	})).Return(&policyv1.SetSubjectLabelsResponse{}, nil)

	err = SetLabels(context.Background(), client, specv1.SubjectKind_SUBJECT_KIND_PROJECT,
		[]string{"p_1", "p_2", "p_1"}, labels)
	require.NoError(t, err)
}

func TestSetLabels_Errors(t *testing.T) {
	client := mocks.NewLabelServiceClientMock(t)
	labels, err := ParseCustomLabels([]string{"custom/a=1"})
	require.NoError(t, err)

	err = SetLabels(context.Background(), client, specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, nil, labels)
	assert.ErrorContains(t, err, "give at least one endpoint")

	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("e_%d", i)
	}
	err = SetLabels(context.Background(), client, specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, many, labels)
	assert.ErrorContains(t, err, "at most 100 subjects")

	client.EXPECT().SetSubjectLabels(mock.Anything, mock.Anything).Return(nil, errors.New("denied"))
	err = SetLabels(context.Background(), client, specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, []string{"e_1"}, labels)
	assert.ErrorContains(t, err, "set labels: denied")
}

func TestDeleteLabels(t *testing.T) {
	client := mocks.NewLabelServiceClientMock(t)
	err := DeleteLabels(context.Background(), client, specv1.SubjectKind_SUBJECT_KIND_PROJECT, []string{"p_1"}, []string{"scm/forge"})
	assert.ErrorContains(t, err, `"scm/" labels`)

	client.EXPECT().DeleteSubjectLabels(mock.Anything, mock.MatchedBy(func(req *policyv1.DeleteSubjectLabelsRequest) bool {
		return req.GetKeys()[0] == "custom/team" && req.GetSubjects()[0].GetId() == "p_1"
	})).Return(&policyv1.DeleteSubjectLabelsResponse{}, nil)
	require.NoError(t, DeleteLabels(context.Background(), client, specv1.SubjectKind_SUBJECT_KIND_PROJECT,
		[]string{"p_1"}, []string{"custom/team"}))
}
