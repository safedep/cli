package policy

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	packageadmissionv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/packageadmission/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

// scriptedEditor plays one edit for each open. An edit gets the file text
// and returns the text to save.
type scriptedEditor struct {
	edits []func(string) string
	seen  []string
}

func (e *scriptedEditor) open(_ context.Context, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	e.seen = append(e.seen, string(b))
	if len(e.edits) == 0 {
		return errors.New("unexpected editor open")
	}
	edit := e.edits[0]
	e.edits = e.edits[1:]
	return os.WriteFile(path, []byte(edit(string(b))), 0o600)
}

func replace(old, new string) func(string) string {
	return func(s string) string { return strings.Replace(s, old, new, 1) }
}

func keep(s string) string { return s }

func sameAs(original string) func([]byte) bool {
	return func(saved []byte) bool { return string(saved) == original }
}

func TestEditLoop_NoChange(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{keep}}
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), sameAs("a: 1\n"), func([]byte) error {
		t.Fatal("save must not run")
		return nil
	})
	assert.ErrorIs(t, err, errNoChange)
}

func TestEditLoop_ReopensWithTheErrorAndSaves(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{
		replace("a: 1", "a: bad"),
		replace("a: bad", "a: 2"),
	}}
	var saved []string
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), sameAs("a: 1\n"), func(text []byte) error {
		saved = append(saved, string(text))
		if strings.Contains(string(text), "bad") {
			return retryable(errors.New("line 1: a: expected a number"))
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a: bad\n", "a: 2\n"}, saved, "the error lines never reach save")
	require.Len(t, ed.seen, 2)
	assert.Contains(t, ed.seen[1], "# ! The last save failed:")
	assert.Contains(t, ed.seen[1], "line 1: a: expected a number")
}

func TestEditLoop_ServerRejectionIsRetryable(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{replace("1", "2"), replace("2", "3")}}
	calls := 0
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), sameAs("a: 1\n"), func([]byte) error {
		calls++
		if calls == 1 {
			return status.Error(codes.InvalidArgument, "override priorities must be unique")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestEditLoop_StopsWhenTheUserGivesUp(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{replace("1", "x"), keep}}
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), sameAs("a: 1\n"), func([]byte) error {
		return retryable(errors.New("bad"))
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the file still has errors")
}

func TestEditLoop_KeepsTheFileOnAnRPCFailure(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{replace("1", "2")}}
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), sameAs("a: 1\n"), func([]byte) error {
		return status.Error(codes.Unavailable, "down")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "down")
}

func TestRunEdit_CreatesAChangeFromTheHead(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	var req *policyv1.CreatePolicyChangeRequest
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			req = r
			c := openChange("18")
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(c)
			return res, nil
		})

	ed := &scriptedEditor{edits: []func(string) string{
		// The first field at this depth is the one of override "payments".
		replace("\n        fail_on: high\n", "\n        fail_on: medium\n"),
	}}
	change, err := runEdit(context.Background(), client, ed.open, "platform-eng.acme.safedep.io",
		editInput{Title: "payments: medium"})
	require.NoError(t, err)
	assert.Equal(t, "18", change.GetChangeId())

	header := ed.seen[0]
	assert.Contains(t, header, "# Policy of platform-eng.acme.safedep.io · head revision 46")
	assert.Contains(t, header, "#   1. SafeDep default policy (read-only, revision 1):")
	assert.Contains(t, header, "scan_gate.fail_on: high")
	assert.Contains(t, header, "# Package rules are not in this file: 211 rules")

	require.NotNil(t, req)
	assert.Equal(t, "payments: medium", req.GetTitle())
	assert.Equal(t, uint64(46), req.GetBaseRevision())
	overrides := req.GetSettings().GetOverrides()
	require.Len(t, overrides, 2)
	assert.Equal(t, "ov_pay", overrides[0].GetOverrideId(), "the edit keeps the ids of the overrides")
	assert.Equal(t, scangatev1.Severity_SEVERITY_MEDIUM, overrides[0].GetSpec().GetScanGate().GetFailOn())
	assert.Equal(t, "ov_ci", overrides[1].GetOverrideId())
}

func TestRunEdit_OneOverride(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	var req *policyv1.CreatePolicyChangeRequest
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			req = r
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(openChange("19"))
			return res, nil
		})

	ed := &scriptedEditor{edits: []func(string) string{replace("fail_on: critical", "fail_on: high")}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{Override: "ci"})
	require.NoError(t, err)
	assert.NotContains(t, ed.seen[0], "payments", "only the named override opens")
	assert.Contains(t, ed.seen[0], "# Policy of this tenant")

	overrides := req.GetSettings().GetOverrides()
	require.Len(t, overrides, 2)
	assert.Equal(t, scangatev1.Severity_SEVERITY_HIGH, overrides[0].GetSpec().GetScanGate().GetFailOn(), "payments stays")
	assert.Equal(t, "ov_ci", overrides[1].GetOverrideId())
	assert.Equal(t, scangatev1.Severity_SEVERITY_HIGH, overrides[1].GetSpec().GetScanGate().GetFailOn())
}

func TestRunEdit_UnknownOverride(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	_, err := runEdit(context.Background(), client, (&scriptedEditor{}).open, "", editInput{Override: "laptops"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no policy override is named "laptops": known overrides: ci, payments`)
}

func TestValidateSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*policyv1.Policy)
		want   string
	}{
		{"valid", func(*policyv1.Policy) {}, ""},
		{"duplicate name", func(p *policyv1.Policy) { p.GetOverrides()[1].SetName("payments") }, `two overrides are named "payments"`},
		{"duplicate priority", func(p *policyv1.Policy) { p.GetOverrides()[1].SetPriority(200) }, "priority 200 is also the priority"},
		{"no priority", func(p *policyv1.Policy) { p.GetOverrides()[0].SetPriority(0) }, "give it a priority"},
		{"no selector", func(p *policyv1.Policy) { p.GetOverrides()[0].ClearSelector() }, "give it a selector"},
		{"no name", func(p *policyv1.Policy) { p.GetOverrides()[0].SetName("") }, "give the override a name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := headPolicy()
			tt.mutate(p)
			err := validateSettings(p)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRunEdit_RenameKeepsTheOverrideID(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	var req *policyv1.CreatePolicyChangeRequest
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			req = r
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(openChange("18"))
			return res, nil
		})

	ed := &scriptedEditor{edits: []func(string) string{replace("name: payments", "name: payments-critical")}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.NoError(t, err)
	assert.Contains(t, ed.seen[0], "  - id: ov_pay\n    name: payments\n")

	o := req.GetSettings().GetOverrides()[0]
	assert.Equal(t, "payments-critical", o.GetName())
	assert.Equal(t, "ov_pay", o.GetOverrideId(), "a rename keeps the id, so it is not a new override")
	assert.NotContains(t, stderr.String(), "This removes")
}

func TestRunEdit_WarnsAboutARemovedOverride(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	var req *policyv1.CreatePolicyChangeRequest
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			req = r
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(openChange("18"))
			return res, nil
		})

	removeCI := func(s string) string {
		i := strings.Index(s, "  - id: ov_ci")
		return s[:i]
	}
	ed := &scriptedEditor{edits: []func(string) string{removeCI}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.NoError(t, err)
	require.Len(t, req.GetSettings().GetOverrides(), 1)
	assert.Contains(t, stderr.String(), `This removes policy override "ci" (id ov_ci)`)
}

func TestRunEdit_RefusesAnIDThatIsNotInThePolicy(t *testing.T) {
	captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	ed := &scriptedEditor{edits: []func(string) string{replace("id: ov_ci", "id: ov_other"), keep}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.Error(t, err)
	assert.Contains(t, ed.seen[1], `id "ov_other" is not in the policy`)
}

func TestRunEdit_RefusesFieldsThatThisCLIDoesNotKnow(t *testing.T) {
	head := headResponse()
	unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 1)
	head.GetPolicy().GetBase().GetScanGate().ProtoReflect().SetUnknown(unknown)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(head, nil)

	_, err := runEdit(context.Background(), client, (&scriptedEditor{}).open, "", editInput{})
	require.Error(t, err)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Contains(t, ue.HumanError(), "the policy holds fields that this safedep CLI does not know: at base.scan_gate")
	assert.Contains(t, ue.Help(), "Update the safedep CLI")
}

func TestRunEdit_PermissionDenied(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).Return(nil, status.Error(codes.PermissionDenied, "member"))

	ed := &scriptedEditor{edits: []func(string) string{replace("fail_on: critical", "fail_on: high")}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.Error(t, err)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, usefulerror.ErrAuthorizationFailed, ue.Code())
	assert.Contains(t, ue.HumanError(), "Your role cannot change the policy")
	assert.Contains(t, ue.Help(), "Ask an Admin of the tenant to do it")
	assert.Contains(t, ue.Help(), "Your edit is in")
}

func TestRunEdit_RewrittenLineEndingsAreNoChange(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	_, err := runEdit(context.Background(), client, (&scriptedEditor{edits: []func(string) string{crlf}}).open, "", editInput{})
	assert.ErrorIs(t, err, errNoChange)
}

func TestRunChangeEdit_RestoreChange(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	c := openChange("20")
	restore := &policyv1.RestoreSource{}
	restore.SetFromRevision(46)
	c.SetRestore(restore)
	client.EXPECT().GetPolicyChange(mock.Anything, mock.Anything).Return(changeResponse(c), nil)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	res := &policyv1.UpdatePolicyChangeResponse{}
	res.SetChange(c)
	client.EXPECT().UpdatePolicyChange(mock.Anything, mock.MatchedBy(func(r *policyv1.UpdatePolicyChangeRequest) bool {
		return r.HasSettings() && r.GetSettings().GetBase().GetScanGate().GetFailOn() == scangatev1.Severity_SEVERITY_LOW
	})).Return(res, nil)

	ed := &scriptedEditor{edits: []func(string) string{replace("\n  scan_gate:\n    fail_on: high", "\n  scan_gate:\n    fail_on: low")}}
	_, err := runChangeEdit(context.Background(), client, ed.open, "", "20", editInput{})
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "Change 20 restores from revision 46. Saving replaces the settings that the restore made")
	assert.Contains(t, ed.seen[0], "fail_on: high", "a change with no settings starts from head")
}

// newTenantResponse is GetPolicy for a tenant that never published: no
// revision and no settings, only the SafeDep default policy.
func newTenantResponse() *policyv1.GetPolicyResponse {
	res := &policyv1.GetPolicyResponse{}
	res.SetDefaultPolicy(headResponse().GetDefaultPolicy())
	return res
}

// uncomment removes the "# " of the first line that starts with line.
func uncomment(lines ...string) func(string) string {
	return func(s string) string {
		for _, line := range lines {
			s = strings.Replace(s, "\n"+line, "\n"+strings.Replace(line, "# ", "", 1), 1)
		}
		return s
	}
}

func createdSettings(client *mocks.PolicyServiceClientMock, out **policyv1.Policy) {
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			*out = r.GetSettings()
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(openChange("18"))
			return res, nil
		})
}

func TestRunEdit_NewTenantDocument(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(newTenantResponse(), nil)
	ed := &scriptedEditor{edits: []func(string) string{keep}}

	_, err := runEdit(context.Background(), client, ed.open, "acme.safedep.io", editInput{})
	require.ErrorIs(t, err, errNoChange, "a save of the template as it is changes nothing")

	// docs/cmd/policy-edit.md shows this document.
	want := `# Policy of acme.safedep.io · head revision 0
# Saving creates a draft change. Nothing applies until you publish it.
#
# Layers, from low to high, for each subject:
#   1. SafeDep default policy (read-only, revision 1):
#        scan_gate.fail_on: high · scan_gate.fail_on_attacks: fail
#   2. base      - applies to every endpoint and project of this tenant
#   3. overrides - apply to the subjects that their selector matches.
#                  For each field, the highest priority that sets it wins.
#
# Keep the id of each override. A new override has no id.
# Package rules are not in this file: 0 rules (safedep policy rule list)
# The policy keeps no comments. Write the reason for a value in a description field.
# To set a field, remove the "# " at the start of its line and of the lines of its parents.
# Fields and values: https://docs.safedep.io/reference/cloud-policy-settings

# base:
  # package_admission:
    # cooldown:
      # min_age: 7d  # a duration, as 7d or 12h, from 0s to 365d. Now: not set
  # scan_gate:
    # fail_on: high  # critical, high, medium, low, info. Now: high (default policy, revision 1)
    # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)

# An example policy override. It applies to the subjects that its selector matches.
# Remove the "# " at the start of each line to use it, then change the values.
# overrides:
#   - name: payments
#     priority: 100
#     selector:
#       matchLabels: {custom/team: payments}
#     spec:
#       package_admission:
#         cooldown:
#           min_age: 168h
`
	require.Len(t, ed.seen, 1)
	assert.Equal(t, want, ed.seen[0])
}

func TestRunEdit_TemplateEdits(t *testing.T) {
	cooldown := func() *policyv1.Policy {
		p := &policyv1.Policy{}
		p.SetBase(&specv1.PolicySpec{})
		c := &packageadmissionv1.Cooldown{}
		c.SetMinAge(durationpb.New(7 * 24 * time.Hour))
		pa := &packageadmissionv1.PackageAdmission{}
		pa.SetCooldown(c)
		p.GetBase().SetPackageAdmission(pa)
		return p
	}
	attacksAtHead := func() *policyv1.Policy {
		p := headPolicy()
		p.GetBase().GetScanGate().SetFailOnAttacks(scangatev1.AttackGate_ATTACK_GATE_REPORT)
		return p
	}
	tests := []struct {
		name string
		head *policyv1.GetPolicyResponse
		edit func(string) string
		want *policyv1.Policy
	}{
		{
			name: "a new tenant sets one field",
			head: newTenantResponse(),
			edit: uncomment("# base:", "  # package_admission:", "    # cooldown:", "      # min_age: 7d"),
			want: cooldown(),
		},
		{
			name: "a tenant with values sets one more field",
			head: headResponse(),
			edit: func(s string) string {
				return strings.Replace(s, "\n    # fail_on_attacks: fail", "\n    fail_on_attacks: report", 1)
			},
			want: attacksAtHead(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := captureStderr(t)
			client := mocks.NewPolicyServiceClientMock(t)
			client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(tt.head, nil)
			var got *policyv1.Policy
			createdSettings(client, &got)

			_, err := runEdit(context.Background(), client, (&scriptedEditor{edits: []func(string) string{tt.edit}}).open,
				"", editInput{})
			require.NoError(t, err)
			assert.True(t, proto.Equal(tt.want, got), "got %v", got)
			assert.NotContains(t, stderr.String(), "The file holds comments", "the notes are the comments of the CLI")
		})
	}
}

func TestRunEdit_TemplateWithAWrongValueReopens(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(newTenantResponse(), nil)
	var got *policyv1.Policy
	createdSettings(client, &got)

	ed := &scriptedEditor{edits: []func(string) string{
		func(s string) string {
			s = uncomment("# base:", "  # scan_gate:")(s)
			return strings.Replace(s, "\n    # fail_on: high", "\n    fail_on: severe", 1)
		},
		replace("fail_on: severe", "fail_on: medium"),
	}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.NoError(t, err)
	require.Len(t, ed.seen, 2)
	assert.Contains(t, ed.seen[1], "# ! The last save failed:")
	assert.Contains(t, ed.seen[1], `unknown value "severe". Use one of: critical, high, medium, low, info`)
	assert.Equal(t, scangatev1.Severity_SEVERITY_MEDIUM, got.GetBase().GetScanGate().GetFailOn())
}

func TestRunEdit_TemplateOfATenantWithValuesIsNoChange(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	ed := &scriptedEditor{edits: []func(string) string{keep}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.ErrorIs(t, err, errNoChange)
	assert.Contains(t, ed.seen[0], "    # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)\n")
	assert.NotContains(t, ed.seen[0], "An example policy override", "the policy has overrides")
}
