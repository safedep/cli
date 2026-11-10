package policy

import (
	"context"
	"strings"
	"testing"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
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
	// The server writes an enum by its value name, with no quotes.
	field.SetOldValue("SEVERITY_HIGH")
	field.SetNewValue("SEVERITY_MEDIUM")
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
	coverage.SetValue("SEVERITY_MEDIUM")
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

func TestNewPreviewView_InheritedValue(t *testing.T) {
	tests := []struct {
		name          string
		layer         recordv1.Layer
		path          string
		oldValue      string
		newValue      string
		inherited     string
		wantCell      string
		wantInherited string
		wantPlain     string
	}{
		{
			name: "base sets a field for the first time", layer: recordv1.Layer_LAYER_BASE,
			path: "scan_gate.fail_on", newValue: "SEVERITY_MEDIUM", inherited: "SEVERITY_HIGH",
			wantCell: "high (default) -> medium", wantInherited: "high",
			wantPlain: "change\tbase\tscan_gate.fail_on\t-\tmedium\tTIGHTENS\thigh",
		},
		{
			name: "override unsets a field", layer: recordv1.Layer_LAYER_OVERRIDE,
			path: "package_admission.cooldown.min_age", oldValue: `"86400s"`, inherited: `"604800s"`,
			wantCell: "1d -> 7d (inherited)", wantInherited: "168h",
			wantPlain: "change\toverride \"payments\"\tpackage_admission.cooldown.min_age\t24h\t-\tTIGHTENS\t168h",
		},
		{
			name: "no lower layer sets the field", layer: recordv1.Layer_LAYER_OVERRIDE,
			path: "package_admission.cooldown.min_age", newValue: `"604800s"`,
			wantCell:  "- -> 7d",
			wantPlain: "change\toverride \"payments\"\tpackage_admission.cooldown.min_age\t-\t168h\tTIGHTENS\t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &policyv1.PathChange{}
			c.SetPath(tt.path)
			c.SetLayer(tt.layer)
			c.SetOverrideName("payments")
			c.SetOldValue(tt.oldValue)
			c.SetNewValue(tt.newValue)
			c.SetInheritedValue(tt.inherited)
			c.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_TIGHTENS)
			res := &policyv1.PreviewPolicyChangeResponse{}
			res.SetChanges([]*policyv1.PathChange{c})

			got := newPreviewView(res)
			require.Len(t, got.Items, 1)
			assert.Equal(t, tt.wantCell, got.Items[0].changeCell())
			assert.Equal(t, tt.wantInherited, got.Items[0].Inherited)
			plain := (&changeShowResult{Preview: got}).RenderPlain()
			assert.Equal(t, tt.wantPlain, plain[strings.LastIndex(plain, "\n")+1:])
		})
	}
}

func TestNewPreviewView_RuleOperationNamesTheHeadRule(t *testing.T) {
	update := &policyv1.PackageRuleUpdate{}
	update.SetRuleId("r_77e")
	updateOp := &policyv1.PackageRuleOperation{}
	updateOp.SetUpdate(update)
	del := &policyv1.PackageRuleDelete{}
	del.SetRuleId("r_80b")
	deleteOp := &policyv1.PackageRuleOperation{}
	deleteOp.SetDelete(del)

	tests := []struct {
		name      string
		op        *policyv1.PackageRuleOperation
		head      *policyv1.PackageRuleWithAttributes
		wantWhere string
		wantWhat  string
	}{
		{
			name: "delete of a base rule", op: deleteOp,
			head:      ruleWithAttributes("r_80b", corev1.Effect_EFFECT_TRUST, ""),
			wantWhere: "package rules", wantWhat: "delete trust pkg:npm/leftpad (r_80b)",
		},
		{
			name: "update of an override rule", op: updateOp,
			head:      ruleWithAttributes("r_77e", corev1.Effect_EFFECT_BLOCK, "ov_pay"),
			wantWhere: "package rules of override ov_pay", wantWhat: "update block pkg:npm/leftpad (r_77e)",
		},
		{
			name: "rule that is not active at head", op: deleteOp,
			wantWhere: "package rules", wantWhat: "delete r_80b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preview := &policyv1.PackageRuleOperationPreview{}
			preview.SetOperation(tt.op)
			preview.SetHeadRule(tt.head)
			res := &policyv1.PreviewPolicyChangeResponse{}
			res.SetPackageRuleOperations([]*policyv1.PackageRuleOperationPreview{preview})

			got := newPreviewView(res)
			require.Len(t, got.Items, 1)
			assert.Equal(t, tt.wantWhere, got.Items[0].Where)
			assert.Equal(t, tt.wantWhat, got.Items[0].What)
		})
	}
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

func TestRunChangeShow_ClosedChangeThatEditedTheSettings(t *testing.T) {
	tests := []struct {
		name      string
		state     policyv1.PolicyChangeState
		published uint64
		settings  bool
		want      string
	}{
		{"published", policyv1.PolicyChangeState_POLICY_CHANGE_STATE_PUBLISHED, 47, true,
			"The change edited the policy settings. See them: safedep policy revision show 47"},
		{"deleted", policyv1.PolicyChangeState_POLICY_CHANGE_STATE_DELETED, 0, true,
			"Only an open change has a preview of them."},
		{"no edit", policyv1.PolicyChangeState_POLICY_CHANGE_STATE_DELETED, 0, false,
			"The change edits nothing."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewPolicyServiceClientMock(t)
			c := openChange("17")
			c.SetState(tt.state)
			c.SetPublishedRevision(tt.published)
			if tt.settings {
				c.SetSettings(headPolicy())
			}
			client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(c), nil)

			got, err := runChangeShow(context.Background(), client, "17")
			require.NoError(t, err)
			assert.Contains(t, got.RenderTable(), tt.want)
		})
	}
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

func TestRunChangeDelete_ChangeThatIsNotOpen(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().DeletePolicyChange(mock.Anything, mock.Anything).
		Return(nil, status.Error(codes.FailedPrecondition, "policy change 19 is not open"))
	err := runChangeDelete(context.Background(), client, "19")
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "The server refused the request: policy change 19 is not open", ue.HumanError())
	assert.Equal(t, "List the open changes: safedep policy change list --state open", ue.Help())
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
	got, err := runChangeEdit(context.Background(), client, ed.open, "", "18", editInput{Title: "new title"})
	require.NoError(t, err)
	assert.False(t, got.titleOnly)
	assert.Contains(t, ed.seen[0], "# Change 18 of this tenant · base revision 46 · head revision 46")
}

func TestRunChangeEdit_TitleWithNoDocumentChange(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		wantUpdate bool
	}{
		{"a new title is saved", "new title", true},
		{"the same title is no change", "payments: medium + cooldown", false},
		{"no title is no change", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewPolicyServiceClientMock(t)
			c := openChange("14")
			c.SetSettings(headPolicy())
			client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(c), nil)
			client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
			if tt.wantUpdate {
				renamed := openChange("14")
				renamed.SetTitle(tt.title)
				res := &policyv1.UpdatePolicyChangeResponse{}
				res.SetChange(renamed)
				client.EXPECT().UpdatePolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.UpdatePolicyChangeRequest) bool {
					return r.GetChangeId() == "14" && r.GetEtag() == "etag-1" && r.GetTitle() == tt.title && !r.HasSettings()
				})).Return(res, nil)
			}

			got, err := runChangeEdit(context.Background(), client, (&scriptedEditor{edits: []func(string) string{keep}}).open,
				"", "14", editInput{Title: tt.title})
			if !tt.wantUpdate {
				assert.ErrorIs(t, err, errNoChange)
				return
			}
			require.NoError(t, err)
			assert.True(t, got.titleOnly)
			assert.Equal(t, tt.title, got.change.GetTitle())
		})
	}
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

func TestReportLoosening(t *testing.T) {
	added := &policyv1.OverrideChange{}
	added.SetOverrideName("policy-test")
	added.SetKind(policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_ADDED)
	added.SetNewValue("policy-test")
	added.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)

	field := &policyv1.PathChange{}
	field.SetPath("scan_gate.fail_on")
	field.SetLayer(recordv1.Layer_LAYER_BASE)
	field.SetNewValue("SEVERITY_LOW")
	field.SetInheritedValue("SEVERITY_HIGH")
	field.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)

	del := &policyv1.PackageRuleDelete{}
	del.SetRuleId("r_80b")
	op := &policyv1.PackageRuleOperation{}
	op.SetDelete(del)
	ruleDelete := &policyv1.PackageRuleOperationPreview{}
	ruleDelete.SetOperation(op)
	ruleDelete.SetHeadRule(ruleWithAttributes("r_80b", corev1.Effect_EFFECT_TRUST, ""))
	ruleDelete.SetDirection(policyv1.ChangeDirection_CHANGE_DIRECTION_LOOSENS)

	tests := []struct {
		name string
		res  func(*policyv1.PreviewPolicyChangeResponse)
		want string
	}{
		{
			name: "an added override",
			res:  func(r *policyv1.PreviewPolicyChangeResponse) { r.SetOverrideChanges([]*policyv1.OverrideChange{added}) },
			want: "It adds policy override \"policy-test\".\n",
		},
		{
			name: "a field with an inherited value",
			res:  func(r *policyv1.PreviewPolicyChangeResponse) { r.SetChanges([]*policyv1.PathChange{field}) },
			want: "It loosens scan_gate.fail_on from high (default) to low in base\n",
		},
		{
			name: "a rule delete",
			res: func(r *policyv1.PreviewPolicyChangeResponse) {
				r.SetPackageRuleOperations([]*policyv1.PackageRuleOperationPreview{ruleDelete})
			},
			want: "It loosens delete trust pkg:npm/leftpad (r_80b) in package rules\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := captureStderr(t)
			res := &policyv1.PreviewPolicyChangeResponse{}
			tt.res(res)
			reportLoosening(newPreviewView(res))
			assert.Contains(t, stderr.String(), tt.want)
		})
	}
}
