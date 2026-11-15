package policy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/output"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

// captureStderr sends the messages of dry/tui to a buffer for one test.
func captureStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	output.SetWriters(os.Stdout, &buf)
	t.Cleanup(func() { output.SetWriters(os.Stdout, os.Stderr) })
	return &buf
}

// refuseConfirm fails the test when the command asks for a confirmation.
func refuseConfirm(t *testing.T) confirmFunc {
	return func(string) (bool, error) {
		t.Fatal("the command must not ask for a confirmation")
		return false, nil
	}
}

func tighteningPreview() *policyv1.PreviewPolicyChangeResponse {
	res := &policyv1.PreviewPolicyChangeResponse{}
	res.SetHeadRevision(46)
	res.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_TIGHTENS)
	return res
}

func publishResponse(direction policyv1.ChangeDirection) *policyv1.PublishPolicyChangeResponse {
	rev := revision(47, "payments")
	rev.SetDirection(direction)
	res := &policyv1.PublishPolicyChangeResponse{}
	res.SetRevision(rev)
	return res
}

func TestRunChangePublish_TighteningNeedsNoConfirmation(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("18")), nil)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(tighteningPreview(), nil)
	client.EXPECT().PublishPolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.PublishPolicyChangeRequest) bool {
		return r.GetChangeId() == "18" && r.GetEtag() == "etag-1" && r.GetMessage() == "payments: fail on medium"
	})).Return(publishResponse(policyv1.ChangeDirection_CHANGE_DIRECTION_TIGHTENS), nil)

	rev, err := runChangePublish(context.Background(), client, "18", "payments: fail on medium", refuseConfirm(t))
	require.NoError(t, err)
	assert.Equal(t, uint64(47), rev.GetRevision())
	assert.Contains(t, stderr.String(), "Change 18 · TIGHTENS. An Admin can publish it")

	reportPublish(rev.GetRevision(), directionToken(rev.GetDirection()))
	assert.Contains(t, stderr.String(), "revision 47 published · TIGHTENS")
	assert.Contains(t, stderr.String(), "safedep endpoint list --applied-revision '<47'")
}

func TestRunChangePublish_ShowsTheDirectionAndEveryWarning(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("18")), nil)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(withEndpointLabelWarning(previewResponse()), nil)
	client.EXPECT().PublishPolicyChange(mock.Anything, mock.Anything).
		Return(publishResponse(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS), nil)

	var asked string
	_, err := runChangePublish(context.Background(), client, "18", "", func(q string) (bool, error) {
		asked = q
		return true, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "Change 18 loosens the policy. Publish it?", asked)

	out := stderr.String()
	assert.Contains(t, out, "Change 18 · LOOSENS. Only an Admin can publish it")
	assert.Contains(t, out, "It loosens package_admission.cooldown.min_age from - to 7d in override \"payments\"")
	assert.Contains(t, out, "It changes the selector of policy override \"payments\" from custom/team=pay to reported/os=linux.")
	assert.Contains(t, out, "ENDPOINT_LABEL_LOOSENS payments: a policy override that loosens selects on a label that an endpoint can send")
}

func TestRunChangePublish_LooseningThatTheUserRefuses(t *testing.T) {
	captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("18")), nil)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(previewResponse(), nil)

	_, err := runChangePublish(context.Background(), client, "18", "", func(string) (bool, error) { return false, nil })
	assert.ErrorIs(t, err, errPublishCancelled)
}

func TestPublishConfirm(t *testing.T) {
	ok, err := publishConfirm(true, false, "", "")("q")
	require.NoError(t, err)
	assert.True(t, ok, "--yes confirms with no terminal")

	_, err = publishConfirm(false, false, "no terminal is present to confirm it", "pass --yes")("q")
	require.Error(t, err)
	ue, isUseful := usefulerror.AsUsefulError(err)
	require.True(t, isUseful)
	assert.Contains(t, ue.HumanError(), "no terminal is present to confirm it")
	assert.Contains(t, ue.Help(), "pass --yes")
}

func TestRunChangePublish_StaleStopsBeforeThePublish(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("19")), nil)
	preview := tighteningPreview()
	preview.SetStale(true)
	preview.SetHeadRevision(51)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(preview, nil)

	_, err := runChangePublish(context.Background(), client, "19", "", refuseConfirm(t))
	require.Error(t, err)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "safedep policy change sync 19", ue.Help()[len(ue.Help())-len("safedep policy change sync 19"):])
	assert.Contains(t, stderr.String(), "Change 19 is stale")
}

func TestRunChangePublish_PreviewFails(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("18")), nil)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(nil, errors.New("down"))
	_, err := runChangePublish(context.Background(), client, "18", "", refuseConfirm(t))
	assert.ErrorContains(t, err, "policy change preview: down")
}

func TestReportPreview_EscapesServerText(t *testing.T) {
	stderr := captureStderr(t)
	preview := newPreviewView(withEndpointLabelWarning(previewResponse()))
	preview.Warnings[0].Message = "evil\x1b]0;title\x07"
	reportPreview("18", preview)
	assert.NotContains(t, stderr.String(), "\x1b")
	assert.Contains(t, stderr.String(), `evil\x1b]0;title\x07`)
}
