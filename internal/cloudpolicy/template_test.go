package cloudpolicy

import (
	"strings"
	"testing"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// defaultPolicy is the SafeDep default policy of control-tower at revision 1.
func defaultPolicy() DefaultLayer {
	spec := specWith(scangatev1.Severity_SEVERITY_HIGH, 0)
	spec.GetScanGate().SetFailOnAttacks(scangatev1.AttackGate_ATTACK_GATE_FAIL)
	return DefaultLayer{Revision: 1, Spec: spec}
}

func TestMarshalPolicyTemplate_NewTenant(t *testing.T) {
	b, err := MarshalPolicyTemplate(&policyv1.Policy{}, "", defaultPolicy())
	require.NoError(t, err)

	want := `# base:
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
	assert.Equal(t, want, string(b))
}

func TestMarshalPolicyTemplate_SetAndUnsetFields(t *testing.T) {
	p := &policyv1.Policy{}
	p.SetBase(specWith(scangatev1.Severity_SEVERITY_MEDIUM, 0))
	selector := &messagescontroltowerv1.LabelSelector{}
	selector.SetMatchLabels(map[string]string{"custom/team": "pay"})
	o := &policyv1.Override{}
	o.SetOverrideId("ov_1")
	o.SetName("payments")
	o.SetPriority(200)
	o.SetSelector(selector)
	o.SetSpec(specWith(scangatev1.Severity_SEVERITY_LOW, 0))
	p.SetOverrides([]*policyv1.Override{o})

	b, err := MarshalPolicyTemplate(p, "", defaultPolicy())
	require.NoError(t, err)
	text := string(b)

	for _, want := range []string{
		"base:\n",
		"\n  scan_gate:\n    fail_on: medium\n",
		"    # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)\n",
		"  # package_admission:\n    # cooldown:\n      # min_age: 7d  #",
		"overrides:\n  - id: ov_1\n    name: payments\n    priority: 200\n",
		"      matchLabels: {custom/team: pay}\n",
		"        fail_on: low\n",
		"        # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)\n",
	} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "An example policy override", "the policy has an override")
	for _, line := range strings.Split(text, "\n") {
		assert.LessOrEqual(t, len(line), templateWidth, line)
	}
}

func TestMarshalPolicyTemplate_TheOverrideNoteReadsTheBase(t *testing.T) {
	p := &policyv1.Policy{}
	p.SetBase(specWith(scangatev1.Severity_SEVERITY_MEDIUM, 0))
	o := &policyv1.Override{}
	o.SetName("payments")
	o.SetPriority(200)
	o.SetSelector(&messagescontroltowerv1.LabelSelector{})
	p.SetOverrides([]*policyv1.Override{o})

	b, err := MarshalPolicyTemplate(p, "", defaultPolicy())
	require.NoError(t, err)
	assert.Contains(t, string(b), "      # fail_on: medium  # critical, high, medium, low, info. Now: medium (base)\n")
	assert.Contains(t, string(b), "    # spec:\n", "an override with no spec gets a commented spec")
}

func TestMarshalPolicyTemplate_RoundTrips(t *testing.T) {
	emptyBase := &policyv1.Policy{}
	emptyBase.SetBase(&specv1.PolicySpec{})

	emptyCapability := &policyv1.Policy{}
	emptyCapability.SetBase(&specv1.PolicySpec{})
	emptyCapability.GetBase().SetScanGate(&scangatev1.ScanGate{})

	emptySpec := samplePolicy()
	emptySpec.GetOverrides()[0].SetSpec(&specv1.PolicySpec{})

	noSpec := samplePolicy()
	noSpec.GetOverrides()[1].ClearSpec()

	tests := []struct {
		name   string
		policy *policyv1.Policy
	}{
		{"set fields", samplePolicy()},
		{"a base with no fields", emptyBase},
		{"a capability with no fields", emptyCapability},
		{"an override spec with no fields", emptySpec},
		{"an override with no spec", noSpec},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := MarshalPolicyTemplate(tt.policy, "", defaultPolicy())
			require.NoError(t, err)
			got, err := UnmarshalPolicy(b)
			require.NoError(t, err)
			assert.True(t, proto.Equal(tt.policy, got), "got %v\n%s", got, b)
			assert.False(t, HasUserComments(b, b), "the template holds only the comments of the CLI")
		})
	}
}

func TestMarshalPolicyTemplate_NewTenantHasNoValues(t *testing.T) {
	b, err := MarshalPolicyTemplate(&policyv1.Policy{}, "# header\n\n", defaultPolicy())
	require.NoError(t, err)
	_, err = UnmarshalPolicy(b)
	assert.ErrorIs(t, err, ErrEmptyDocument, "a comment is never a value")
}

func TestMarshalPolicyTemplate_UncommentOneField(t *testing.T) {
	b, err := MarshalPolicyTemplate(&policyv1.Policy{}, "", defaultPolicy())
	require.NoError(t, err)
	text := string(b)
	for _, line := range []string{"# base:", "  # scan_gate:", "    # fail_on_attacks: fail"} {
		uncommented := strings.Replace(line, "# ", "", 1)
		text = strings.Replace(text, line, uncommented, 1)
	}

	got, err := UnmarshalPolicy([]byte(text))
	require.NoError(t, err)
	gate := &scangatev1.ScanGate{}
	gate.SetFailOnAttacks(scangatev1.AttackGate_ATTACK_GATE_FAIL)
	want := &policyv1.Policy{}
	want.SetBase(&specv1.PolicySpec{})
	want.GetBase().SetScanGate(gate)
	assert.True(t, proto.Equal(want, got), "got %v", got)
	assert.False(t, HasUserComments([]byte(text), b), "the note of an uncommented line is the CLI's")
}

func TestMarshalOverrideTemplate(t *testing.T) {
	p := samplePolicy()
	b, err := MarshalOverrideTemplate(p.GetOverrides()[0], "", defaultPolicy(), p.GetBase())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(b), "id: ov_2\nname: ci\n"))
	assert.Contains(t, string(b), "\n    # fail_on: high  # critical, high, medium, low, info. Now: high (base)\n")

	got, err := UnmarshalOverride(b)
	require.NoError(t, err)
	assert.True(t, proto.Equal(p.GetOverrides()[0], got))
}

func TestHasUserComments_Template(t *testing.T) {
	written, err := MarshalPolicyTemplate(samplePolicy(), CommentBlock([]string{"Policy of acme"}), defaultPolicy())
	require.NoError(t, err)
	text := string(written)
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"unchanged", text, false},
		{"a comment of the user", strings.Replace(text, "fail_on: high\n", "fail_on: high # CUST-412\n", 1), true},
		{"a changed note", strings.Replace(text, "Now: fail", "Now: maybe", 1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, HasUserComments([]byte(tt.text), written))
		})
	}
}

func TestValueForm(t *testing.T) {
	md := (&specv1.PolicySpec{}).ProtoReflect().Descriptor()
	tests := []struct {
		path string
		want string
	}{
		{"scan_gate.fail_on", "critical, high, medium, low, info"},
		{"package_admission.cooldown.min_age", "a duration, as 7d or 12h, from 0s to 365d"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			fd := fieldByPath(tt.path)
			require.NotNil(t, fd, md.FullName())
			assert.Equal(t, tt.want, valueForm(fd))
		})
	}
}
