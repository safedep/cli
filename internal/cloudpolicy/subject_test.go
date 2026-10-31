package cloudpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeSubjectPolicyClient is a hand fake for the one RPC of
// SubjectPolicyClient.
type fakeSubjectPolicyClient struct {
	req *policyv1.GetSubjectPolicyRequest
	res *policyv1.GetSubjectPolicyResponse
	err error
}

func (f *fakeSubjectPolicyClient) GetSubjectPolicy(
	_ context.Context,
	req *policyv1.GetSubjectPolicyRequest,
	_ ...grpc.CallOption,
) (*policyv1.GetSubjectPolicyResponse, error) {
	f.req = req
	return f.res, f.err
}

func fieldSource(path string, layer recordv1.Layer, overrideID string) *specv1.FieldSource {
	s := &specv1.FieldSource{}
	s.SetPath(path)
	s.SetLayer(layer)
	s.SetOverrideId(overrideID)
	return s
}

func subjectPolicyResponse() *policyv1.GetSubjectPolicyResponse {
	spec := specWith(scangatev1.Severity_SEVERITY_MEDIUM, 168*time.Hour)
	block := &corev1.PackageRule{}
	block.SetEffect(corev1.Effect_EFFECT_BLOCK)
	trust := &corev1.PackageRule{}
	trust.SetEffect(corev1.Effect_EFFECT_TRUST)
	core := &corev1.Core{}
	core.SetPackageRules([]*corev1.PackageRule{block, block, trust})
	spec.SetCore(core)

	matched := &specv1.MatchedOverride{}
	matched.SetOverrideId("ov_1")
	matched.SetName("payments")
	matched.SetPriority(200)
	header := &specv1.ResolvedPolicyHeader{}
	header.SetRevision(47)
	header.SetDefaultPolicyRevision(12)
	header.SetMatchedOverrides([]*specv1.MatchedOverride{matched})

	intended := &specv1.ResolvedPolicy{}
	intended.SetHeader(header)
	intended.SetSpec(spec)
	intended.SetFieldSources([]*specv1.FieldSource{
		fieldSource("scan_gate.fail_on", recordv1.Layer_LAYER_OVERRIDE, "ov_1"),
		fieldSource("package_admission.cooldown.min_age", recordv1.Layer_LAYER_BASE, ""),
	})

	exception := &recordv1.FieldException{}
	exception.SetPath("package_admission.cooldown.min_age")
	exception.SetReason(recordv1.FieldExceptionReason_FIELD_EXCEPTION_REASON_LOCKED)
	exception.SetDetail("floor in /etc/safedep/cloud.yml")
	report := &recordv1.ApplyReport{}
	report.SetRevision(46)
	report.SetState(recordv1.ApplyState_APPLY_STATE_APPLIED)
	report.SetExceptions([]*recordv1.FieldException{exception})
	applied := &policyv1.AppliedPolicy{}
	applied.SetImplementer("pmg")
	applied.SetImplementerVersion("2.3.1")
	applied.SetReport(report)
	applied.SetReportedAt(timestamppb.New(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)))

	label := &messagescontroltowerv1.Label{}
	label.SetKey("custom/criticality")
	label.SetValue("high")

	res := &policyv1.GetSubjectPolicyResponse{}
	res.SetIntended(intended)
	res.SetApplied([]*policyv1.AppliedPolicy{applied})
	res.SetLabels([]*messagescontroltowerv1.Label{label})
	return res
}

func TestGetSubjectPolicy(t *testing.T) {
	client := &fakeSubjectPolicyClient{res: subjectPolicyResponse()}
	subject := NewSubject(specv1.SubjectKind_SUBJECT_KIND_PROJECT, "p_1")

	got, err := GetSubjectPolicy(context.Background(), client, subject, "acme/payments-api", nil)
	require.NoError(t, err)

	assert.Equal(t, specv1.SubjectKind_SUBJECT_KIND_PROJECT, client.req.GetSubject().GetKind())
	assert.Equal(t, "p_1", client.req.GetSubject().GetId())
	assert.Equal(t, "project", got.Kind)
	assert.Equal(t, uint64(47), got.Revision)
	assert.Equal(t, 1, got.TrustRules)
	assert.Equal(t, 2, got.BlockRules)
	assert.Equal(t, []string{"custom/criticality=high"}, got.Labels)
	assert.Equal(t, "revision 47 · overrides payments (200)", got.PolicyLine())
	require.Len(t, got.Fields, 2)
	assert.Equal(t, SubjectField{
		Path: "package_admission.cooldown.min_age", Value: "168h", Human: "7d", Source: "base",
		Exceptions: []string{"pmg: locked (floor in /etc/safedep/cloud.yml)"},
	}, got.Fields[0])
	assert.Equal(t, `override "payments" (200)`, got.Fields[1].Source)
	require.Len(t, got.Applied, 1)
	assert.Equal(t, "applied", got.Applied[0].State)

	table := got.RenderTable()
	assert.Contains(t, table, "scan_gate.fail_on")
	assert.Contains(t, table, "pmg")

	var decoded map[string]any
	b, err := got.RenderJSON()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, "p_1", decoded["id"])
	assert.Contains(t, got.RenderPlain(), "scan_gate.fail_on\tmedium\toverride \"payments\" (200)")
}

func TestSubjectPolicy_EscapesServerText(t *testing.T) {
	res := subjectPolicyResponse()
	label := res.GetLabels()[0]
	label.SetValue("high\x1b[2J\x1b]0;pwned\x07")
	got, err := GetSubjectPolicy(context.Background(), &fakeSubjectPolicyClient{res: res},
		NewSubject(specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, "ep_1"), "host\nname", nil)
	require.NoError(t, err)

	for _, out := range []string{got.RenderTable(), got.RenderPlain()} {
		assert.NotContains(t, out, "\x1b")
		assert.NotContains(t, out, "\x07")
	}
	assert.Contains(t, got.RenderTable(), `custom/criticality=high\x1b[2J\x1b]0;pwned\x07`)
	assert.Contains(t, got.RenderTable(), `host\nname`)

	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `\u001b[2J`, "JSON keeps the raw value, escaped by the JSON encoder")
}

func TestGetSubjectPolicy_FieldFilter(t *testing.T) {
	client := &fakeSubjectPolicyClient{res: subjectPolicyResponse()}
	subject := NewSubject(specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, "ep_1")

	got, err := GetSubjectPolicy(context.Background(), client, subject, "", []string{"scan_gate"})
	require.NoError(t, err)
	require.Len(t, got.Fields, 1)
	assert.Equal(t, "scan_gate.fail_on", got.Fields[0].Path)
	assert.True(t, got.Filtered)
}

func TestGetSubjectPolicy_NoOverrideMatches(t *testing.T) {
	res := subjectPolicyResponse()
	res.GetIntended().GetHeader().SetMatchedOverrides(nil)
	got, err := GetSubjectPolicy(context.Background(), &fakeSubjectPolicyClient{res: res},
		NewSubject(specv1.SubjectKind_SUBJECT_KIND_PROJECT, "p_1"), "", nil)
	require.NoError(t, err)
	assert.Equal(t, "revision 47 · no override matches · base only", got.PolicyLine())
	assert.Equal(t, "override ov_1", got.Fields[1].Source, "an unknown override shows its id")
}

func TestGetSubjectPolicy_Errors(t *testing.T) {
	subject := NewSubject(specv1.SubjectKind_SUBJECT_KIND_PROJECT, "p_1")
	_, err := GetSubjectPolicy(context.Background(), &fakeSubjectPolicyClient{}, subject, "", []string{"nope"})
	assert.ErrorContains(t, err, "unknown policy field")

	_, err = GetSubjectPolicy(context.Background(), &fakeSubjectPolicyClient{err: errors.New("boom")}, subject, "", nil)
	assert.ErrorContains(t, err, "get subject policy: boom")
}
