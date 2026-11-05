package policy

import (
	"context"
	"testing"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func changeResponse(c *policyv1.PolicyChange) *policyv1.GetPolicyChangeResponse {
	res := &policyv1.GetPolicyChangeResponse{}
	res.SetChange(c)
	return res
}

func TestRunChangeList(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	published := openChange("17")
	published.SetState(policyv1.PolicyChangeState_POLICY_CHANGE_STATE_PUBLISHED)
	published.SetPublishedRevision(46)
	published.SetTitle("")
	res := &policyv1.ListPolicyChangesResponse{}
	res.SetChanges([]*policyv1.PolicyChange{openChange("18"), published})

	client.EXPECT().ListPolicyChanges(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPolicyChangesRequest) bool {
		return r.GetFilters().GetState() == policyv1.PolicyChangeState_POLICY_CHANGE_STATE_OPEN &&
			r.GetPagination().GetPageSize() == 5
	})).Return(res, nil)

	got, err := runChangeList(context.Background(), client, changeListInput{State: "open", PageSize: 5})
	require.NoError(t, err)
	require.Len(t, got.Changes, 2)
	table := got.RenderTable()
	assert.Contains(t, table, "payments: medium + cooldown")
	assert.Contains(t, table, "(untitled)")
	assert.Contains(t, table, "published as rev 46")

	_, err = runChangeList(context.Background(), client, changeListInput{State: "draft"})
	assert.ErrorContains(t, err, `unknown --state value "draft": allowed values are open, published, deleted`)
}

// previewResponse is the preview that control-tower gives for change 18. The
// policy base and the default policy set no cooldown, so a cooldown that an
// override sets for the first time loosens: the server cannot know what a
// tool did before. A selector edit always loosens. The pull request check
// enforces scan_gate.fail_on and does not support the cooldown.
func previewResponse() *policyv1.PreviewPolicyChangeResponse {
	field := &policyv1.PathChange{}
	field.SetPath("scan_gate.fail_on")
	field.SetLayer(recordv1.Layer_LAYER_OVERRIDE)
	field.SetOverrideName("payments")
	field.SetOldValue(`"SEVERITY_HIGH"`)
	field.SetNewValue(`"SEVERITY_MEDIUM"`)
	field.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_TIGHTENS)

	cooldown := &policyv1.PathChange{}
	cooldown.SetPath("package_admission.cooldown.min_age")
	cooldown.SetLayer(recordv1.Layer_LAYER_OVERRIDE)
	cooldown.SetOverrideName("payments")
	cooldown.SetNewValue(`"604800s"`)
	cooldown.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)

	selector := &policyv1.OverrideChange{}
	selector.SetOverrideName("payments")
	selector.SetKind(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_SELECTOR)
	// The server writes a selector as protojson.
	selector.SetOldValue(`{"matchLabels":{"custom/team":"pay"}}`)
	selector.SetNewValue(`{"matchLabels":{"custom/criticality":"high"}}`)
	selector.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)

	cooldownCoverage := &policyv1.CoverageSummary{}
	cooldownCoverage.SetPath("package_admission.cooldown.min_age")
	cooldownCoverage.SetValue(`"604800s"`)
	cooldownCoverage.SetUnsupportedCount(3)
	coverage := &policyv1.CoverageSummary{}
	coverage.SetPath("scan_gate.fail_on")
	coverage.SetValue(`"SEVERITY_MEDIUM"`)
	coverage.SetEnforceCount(3)

	res := &policyv1.PreviewPolicyChangeResponse{}
	res.SetHeadRevision(46)
	res.SetChanges([]*policyv1.PathChange{field, cooldown})
	res.SetOverrideChanges([]*policyv1.OverrideChange{selector})
	res.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)
	res.SetMatchedProjectCount(3)
	res.SetCoverage([]*policyv1.CoverageSummary{cooldownCoverage, coverage})
	return res
}

// withEndpointLabelWarning moves the selector of "payments" to a label that an
// endpoint sends, and adds the warning that the server gives for it.
func withEndpointLabelWarning(res *policyv1.PreviewPolicyChangeResponse) *policyv1.PreviewPolicyChangeResponse {
	res.GetOverrideChanges()[0].SetNewValue(`{"matchLabels":{"reported/os":"linux"}}`)
	warning := &policyv1.PreviewWarning{}
	warning.SetCode(policyv1.PreviewWarningCode_PREVIEW_WARNING_CODE_ENDPOINT_LABEL_LOOSENS)
	warning.SetSubject("payments")
	warning.SetMessage("a policy override that loosens selects on a label that an endpoint can send")
	res.SetWarnings([]*policyv1.PreviewWarning{warning})
	return res
}

func TestRunChangeShow_PreviewsAnOpenChange(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("18")), nil)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.PreviewPolicyChangeRequest) bool {
		return r.GetChangeId() == "18"
	})).Return(previewResponse(), nil)

	got, err := runChangeShow(context.Background(), client, "18")
	require.NoError(t, err)
	table := got.RenderTable()
	for _, want := range []string{
		`Change 18 "payments: medium + cooldown" · open · base rev 46 · head rev 46 · no conflict`,
		`override "payments"`,
		"high -> medium",
		"- -> 7d",
		"custom/team=pay -> custom/criticality=high",
		"No warnings.",
		"subjects matched: 3 projects, 0 endpoints",
		"scan_gate.fail_on = medium",
		"package_admission.cooldown.min_age = 168h",
		"LOOSENS. Only an Admin can publish it",
	} {
		assert.Contains(t, table, want)
	}
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"direction": "LOOSENS"`)
	plain := got.RenderPlain()
	assert.Contains(t, plain, "change_id\t18\ntitle\tpayments: medium + cooldown\nstate\topen\nbase_revision\t46")
	assert.Contains(t, plain, "change\toverride \"payments\"\tscan_gate.fail_on\thigh\tmedium\tTIGHTENS")
	assert.Contains(t, plain, "change\toverride \"payments\"\tselector\t{\"matchLabels\":{\"custom/team\":\"pay\"}}\t", "plain keeps the value of the server")
}

func TestRunChangeShow_Warnings(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("18")), nil)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(withEndpointLabelWarning(previewResponse()), nil)

	got, err := runChangeShow(context.Background(), client, "18")
	require.NoError(t, err)
	table := got.RenderTable()
	assert.Contains(t, table, "custom/team=pay -> reported/os=linux")
	assert.Contains(t, table, "ENDPOINT_LABEL_LOOSENS")
	assert.Contains(t, got.RenderPlain(),
		"warning\tENDPOINT_LABEL_LOOSENS\tpayments\ta policy override that loosens selects on a label that an endpoint can send")
}

func TestRunChangeShow_StaleChange(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("19")), nil)
	preview := previewResponse()
	preview.SetStale(true)
	preview.SetHeadRevision(51)
	client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(preview, nil)

	got, err := runChangeShow(context.Background(), client, "19")
	require.NoError(t, err)
	assert.Contains(t, got.RenderTable(), "head rev 51 · STALE: sync it before you publish")
}

func TestRunChangeShow_PublishedChangeHasNoPreview(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	c := openChange("17")
	c.SetState(policyv1.PolicyChangeState_POLICY_CHANGE_STATE_PUBLISHED)
	c.SetPublishedRevision(47)
	del := &policyv1.PackageRuleDelete{}
	del.SetRuleId("r_80b")
	op := &policyv1.PackageRuleOperation{}
	op.SetDelete(del)
	c.SetPackageRuleOperations([]*policyv1.PackageRuleOperation{op})
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(c), nil)

	got, err := runChangeShow(context.Background(), client, "17")
	require.NoError(t, err)
	assert.Nil(t, got.Preview)
	assert.Contains(t, got.RenderTable(), "delete r_80b")
	assert.Contains(t, got.RenderTable(), "published as rev 47")
}

func TestRunChangePublish_Errors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantText string
		wantHelp string
	}{
		{
			name:     "stale",
			err:      reasonError(codes.Aborted, errorv1.ErrorReason_ERROR_REASON_POLICY_CHANGE_STALE),
			wantText: "The policy moved",
			wantHelp: "safedep policy change sync 19",
		},
		{
			name:     "etag",
			err:      reasonError(codes.Aborted, errorv1.ErrorReason_ERROR_REASON_POLICY_CHANGE_ETAG_MISMATCH),
			wantText: "Another request updated change 19",
			wantHelp: "safedep policy change show 19",
		},
		{
			name:     "role",
			err:      status.Error(codes.PermissionDenied, "no"),
			wantText: "Your role cannot change the policy",
			wantHelp: "Ask an Admin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captureStderr(t)
			client := mocks.NewPolicyServiceClientMock(t)
			client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("19")), nil)
			client.EXPECT().PreviewPolicyChange(mock.Anything, mock.Anything).Return(tighteningPreview(), nil)
			client.EXPECT().PublishPolicyChange(mock.Anything, mock.Anything).Return(nil, tt.err)

			_, err := runChangePublish(context.Background(), client, "19", "", refuseConfirm(t))
			require.Error(t, err)
			ue, ok := usefulerror.AsUsefulError(err)
			require.True(t, ok)
			assert.Contains(t, ue.HumanError(), tt.wantText)
			assert.Contains(t, ue.Help(), tt.wantHelp)
		})
	}
}

func TestRunChangePublish_OnlyAnOpenChange(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	c := openChange("17")
	c.SetState(policyv1.PolicyChangeState_POLICY_CHANGE_STATE_DELETED)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(c), nil)

	_, err := runChangePublish(context.Background(), client, "17", "", refuseConfirm(t))
	assert.ErrorContains(t, err, "change 17 is deleted: you can change only an open change")
}

func TestRunChangeGet_NotFound(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(nil, status.Error(codes.NotFound, "x"))
	_, err := runChangeSync(context.Background(), client, "99")
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "Policy change 99 does not exist", ue.HumanError())
}

func TestRunChangeSync(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(openChange("19")), nil)
	synced := openChange("19")
	synced.SetBaseRevision(51)
	res := &policyv1.SyncPolicyChangeResponse{}
	res.SetChange(synced)
	client.EXPECT().SyncPolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.SyncPolicyChangeRequest) bool {
		return r.GetEtag() == "etag-1"
	})).Return(res, nil)

	got, err := runChangeSync(context.Background(), client, "19")
	require.NoError(t, err)
	assert.Equal(t, uint64(51), got.GetBaseRevision())
}

func TestRunChangeDelete(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().DeletePolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.DeletePolicyChangeRequest) bool {
		return r.GetChangeId() == "19"
	})).Return(&policyv1.DeletePolicyChangeResponse{}, nil)
	require.NoError(t, runChangeDelete(context.Background(), client, "19"))
}

func TestRunChangeEdit_UpdatesWithTheEtag(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	c := openChange("18")
	c.SetSettings(headPolicy())
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(c), nil)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	res := &policyv1.UpdatePolicyChangeResponse{}
	res.SetChange(c)
	client.EXPECT().UpdatePolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.UpdatePolicyChangeRequest) bool {
		return r.GetEtag() == "etag-1" && r.GetTitle() == "new title" && r.HasSettings() &&
			r.GetSettings().GetOverrides()[0].GetDescription() == "why"
	})).Return(res, nil)

	ed := &scriptedEditor{edits: []func(string) string{replace("    name: payments\n", "    name: payments\n    description: why\n")}}
	_, err := runChangeEdit(context.Background(), client, ed.open, "", "18", editInput{Title: "new title"})
	require.NoError(t, err)
	assert.Contains(t, ed.seen[0], "# Change 18 of this tenant · base revision 46 · head revision 46")
}

func TestChangeCreate(t *testing.T) {
	assert.ErrorContains(t, validateChangeCreate(changeCreateInput{PackageRules: "head"}), "--from-revision is required")
	assert.ErrorContains(t, validateChangeCreate(changeCreateInput{FromRevision: 46, PackageRules: "head", Capabilities: []string{"core"}}),
		`unknown --capability "core"`)
	assert.ErrorContains(t, validateChangeCreate(changeCreateInput{FromRevision: 46, PackageRules: "all"}), "unknown --package-rules")

	client := mocks.NewPolicyServiceClientMock(t)
	created := openChange("20")
	restore := &policyv1.RestoreSource{}
	restore.SetFromRevision(46)
	restore.SetCapabilities([]string{"scan_gate"})
	created.SetRestore(restore)
	res := &policyv1.CreatePolicyChangeResponse{}
	res.SetChange(created)
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.CreatePolicyChangeRequest) bool {
		return !r.HasSettings() && r.GetRestore().GetFromRevision() == 46 &&
			r.GetRestore().GetCapabilities()[0] == "scan_gate" &&
			r.GetRestore().GetPackageRules() == policyv1.PackageRuleRestore_PACKAGE_RULE_RESTORE_KEEP_HEAD &&
			r.GetTitle() == "revert fail_on to high"
	})).Return(res, nil)

	got, err := runChangeCreate(context.Background(), client, changeCreateInput{
		FromRevision: 46, Capabilities: []string{"scan_gate"}, PackageRules: "head", Title: "revert fail_on to high",
	})
	require.NoError(t, err)
	assert.Equal(t, "restores scan_gate from revision 46 · package rules stay at head", restoreSummary(got.GetRestore()))
}

func TestOverrideChangeValue(t *testing.T) {
	selector := policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_SELECTOR
	tests := []struct {
		name  string
		kind  policyv1.OverrideChangeKind
		value string
		want  string
	}{
		{"match labels", selector, `{"matchLabels":{"custom/team":"pay","custom/criticality":"high"}}`, "custom/criticality=high, custom/team=pay"},
		{"match expressions", selector, `{"matchExpressions":[{"key":"scm/owner","operator":"LABEL_SELECTOR_OPERATOR_IN","values":["acme"]}]}`, "scm/owner in (acme)"},
		{"no value", selector, "", "-"},
		{"not protojson", selector, "custom/team=pay", "custom/team=pay"},
		{"another kind", policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_PRIORITY, "200", "200"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, overrideChangeValue(tt.kind, tt.value))
		})
	}
}

func TestOverrideChangeItem(t *testing.T) {
	change := func(kind policyv1.OverrideChangeKind, old, new string) *policyv1.OverrideChange {
		c := &policyv1.OverrideChange{}
		c.SetOverrideName("payments")
		c.SetKind(kind)
		c.SetOldValue(old)
		c.SetNewValue(new)
		c.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)
		return c
	}
	tests := []struct {
		name     string
		change   *policyv1.OverrideChange
		what     string
		cell     string
		sentence string
	}{
		{"added", change(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_ADDED, "", "payments"),
			"new policy override", "-", `It adds policy override "payments".`},
		{"removed", change(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_REMOVED, "payments", ""),
			"removed policy override", "-", `It removes policy override "payments".`},
		{"selector", change(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_SELECTOR,
			`{"matchLabels":{"custom/team":"pay"}}`, `{"matchLabels":{"custom/criticality":"high"}}`),
			"selector", "custom/team=pay -> custom/criticality=high",
			`It changes the selector of policy override "payments" from custom/team=pay to custom/criticality=high.`},
		{"priority", change(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_PRIORITY, "100", "200"),
			"priority", "100 -> 200", `It changes the priority of policy override "payments" from 100 to 200.`},
		{"expiry", change(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_EXPIRY, "", `"2027-01-26T00:00:00Z"`),
			"expiry", "none -> 2027-01-26T00:00:00Z",
			`It changes the expiry of policy override "payments" from none to 2027-01-26T00:00:00Z.`},
		{"name", change(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_NAME, "pay", "payments"),
			"name", "pay -> payments", `It renames policy override "pay" to "payments".`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := overrideChangeItem(tt.change)
			assert.Equal(t, tt.what, item.whatCell())
			assert.Equal(t, tt.cell, item.changeCell())
			assert.Equal(t, tt.sentence, item.sentence)
			assert.Equal(t, tt.change.GetOldValue(), item.Old, "plain and JSON keep the value of the server")
		})
	}
}

func TestReportLoosening_AnAddedOverride(t *testing.T) {
	stderr := captureStderr(t)
	c := &policyv1.OverrideChange{}
	c.SetOverrideName("policy-test")
	c.SetKind(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_ADDED)
	c.SetNewValue("policy-test")
	c.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)
	res := &policyv1.PreviewPolicyChangeResponse{}
	res.SetOverrideChanges([]*policyv1.OverrideChange{c})
	res.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)

	reportLoosening(newPreviewView(res))
	assert.Contains(t, stderr.String(), `It adds policy override "policy-test".`)
	assert.NotContains(t, stderr.String(), "It loosens added")
}
