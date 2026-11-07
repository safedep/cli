package cloudpolicy

import (
	"strings"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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

	// docs/cmd/policy-edit.md shows this document after the header.
	want := `# base:
  # package_admission:
    # cooldown:
      # a duration, as 7d or 12h, from 0s to 365d. Now: not set, each tool uses its local value
      # min_age: 7d
  # scan_gate:
    # fail_on: high  # critical, high, medium, low, info. Now: high (default policy, revision 1)
    # fail_on_attacks: fail  # report, fail. Now: fail (default policy, revision 1)
    # licenses:
      # The licenses that fail the gate. A list replaces the list below it. deny: {licenses: []}
      # denies nothing. Values: {licenses: [text, ...]}. Now: not set, each tool uses its local
      # value
      # deny:
      # With entries, a license outside the list fails the gate. A list replaces the list below it.
      # An empty list turns the allow list off. Values: {licenses: [text, ...]}. Now: not set, each
      # tool uses its local value
      # allow:
      # What the gate does on a package with an unknown license that the lists do not fail. Values:
      # allow, report, fail. Now: not set, each tool uses its local value
      # unknown: allow
# base_description: Why the base has these values  # text

# An example policy override. It applies to the subjects that its selector matches.
# Remove the "# " at the start of each line to use it, then change the values.
# overrides:
#   - name: payments
#     description: A longer cooldown for the payments team
#     priority: 100
#     selector:
#       matchLabels: {custom/team: payments}
#     spec:
#       package_admission:
#         cooldown:
#           min_age: 7d
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
		"  # package_admission:\n    # cooldown:\n      # a duration, as 7d or 12h, from 0s to 365d. Now: not set, each tool uses its local value\n      # min_age: 7d\n",
		"# base_description: Why the base has these values  # text\n",
		"overrides:\n  - id: ov_1\n    name: payments\n    priority: 200\n",
		"      matchLabels: {custom/team: pay}\n",
		"        fail_on: low\n",
		"          # min_age: 7d  # a duration, as 7d or 12h, from 0s to 365d. Below this override: not set\n",
		"        # report, fail. Below this override: fail (default policy, revision 1)\n        # fail_on_attacks: fail\n",
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
	assert.Contains(t, string(b), "        # fail_on: medium  # critical, high, medium, low, info. Below this override: medium (base)\n")
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
	assert.Contains(t, string(b), "\n    # fail_on: high  # critical, high, medium, low, info. Below this override: high (base)\n")

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

func TestSelectorExpressions_RoundTrip(t *testing.T) {
	sel, err := ParseSelector("scm/forge=github, custom/tier in (a,b), !custom/legacy, custom/owner")
	require.NoError(t, err)
	o := &policyv1.Override{}
	o.SetName("mixed")
	o.SetPriority(10)
	o.SetSelector(sel)
	o.SetExpiresAt(timestamppb.New(time.Date(2027, 1, 26, 0, 0, 0, 0, time.UTC)))

	b, err := MarshalOverrideTemplate(o, "", DefaultLayer{}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(b), "operator: In")
	assert.Contains(t, string(b), "expires_at: 2027-01-26T00:00:00Z")

	got, err := UnmarshalOverride(b)
	require.NoError(t, err)
	assert.True(t, proto.Equal(o, got), "got %v", got)
}

func TestEmptySelectorKeepsItsPresence(t *testing.T) {
	o := &policyv1.Override{}
	o.SetName("all")
	o.SetPriority(1)
	o.SetSelector(&messagescontroltowerv1.LabelSelector{})
	b, err := MarshalOverrideTemplate(o, "", DefaultLayer{}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(b), "selector: {}")

	got, err := UnmarshalOverride(b)
	require.NoError(t, err)
	assert.True(t, got.HasSelector(), "selector: {} selects every subject and must stay set")
}

func TestPruneEmpty(t *testing.T) {
	emptyBase := &policyv1.Policy{}
	emptyBase.SetBase(&specv1.PolicySpec{})
	emptyBase.GetBase().SetScanGate(&scangatev1.ScanGate{})

	zeroCooldown := &policyv1.Policy{}
	cooldownSpec := samplePolicy().GetBase()
	cooldownSpec.GetPackageAdmission().GetCooldown().SetMinAge(durationpb.New(0))
	zeroCooldown.SetBase(cooldownSpec)

	everySubject := &policyv1.Policy{}
	o := &policyv1.Override{}
	o.SetName("all")
	o.SetSelector(&messagescontroltowerv1.LabelSelector{})
	o.SetSpec(&specv1.PolicySpec{})
	everySubject.SetOverrides([]*policyv1.Override{o})

	tests := []struct {
		name  string
		in    *policyv1.Policy
		check func(t *testing.T, got *policyv1.Policy)
	}{
		{"empty messages go", emptyBase, func(t *testing.T, got *policyv1.Policy) {
			assert.False(t, got.HasBase())
		}},
		{"a zero duration stays", zeroCooldown, func(t *testing.T, got *policyv1.Policy) {
			assert.True(t, got.GetBase().GetPackageAdmission().GetCooldown().HasMinAge(), "0s turns the cooldown off")
		}},
		{"an empty selector stays", everySubject, func(t *testing.T, got *policyv1.Policy) {
			assert.True(t, got.GetOverrides()[0].HasSelector(), "an empty selector selects every subject")
			assert.False(t, got.GetOverrides()[0].HasSpec())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := proto.Clone(tt.in)
			got, ok := PruneEmpty(tt.in).(*policyv1.Policy)
			require.True(t, ok)
			tt.check(t, got)
			assert.True(t, proto.Equal(before, tt.in), "PruneEmpty does not change its input")
		})
	}
}

// licenseList builds a list setting. With no licenses it is set and empty.
func licenseList(licenses ...string) *scangatev1.LicenseList {
	l := &scangatev1.LicenseList{}
	l.SetLicenses(licenses)
	return l
}

func specWithLicenses(deny, allow *scangatev1.LicenseList, unknown scangatev1.UnknownLicenseGate) *specv1.PolicySpec {
	licenses := &scangatev1.LicenseGate{}
	if deny != nil {
		licenses.SetDeny(deny)
	}
	if allow != nil {
		licenses.SetAllow(allow)
	}
	if unknown != scangatev1.UnknownLicenseGate_UNKNOWN_LICENSE_GATE_UNSPECIFIED {
		licenses.SetUnknown(unknown)
	}
	gate := &scangatev1.ScanGate{}
	gate.SetLicenses(licenses)
	spec := &specv1.PolicySpec{}
	spec.SetScanGate(gate)
	return spec
}

func TestMarshalPolicyTemplate_LicenseGateOfTheDefaultPolicy(t *testing.T) {
	def := DefaultLayer{
		Revision: 2,
		Spec: specWithLicenses(licenseList("GPL-2.0-only", "GPL-3.0-only"), nil,
			scangatev1.UnknownLicenseGate_UNKNOWN_LICENSE_GATE_ALLOW),
	}
	b, err := MarshalPolicyTemplate(&policyv1.Policy{}, "", def)
	require.NoError(t, err)
	text := string(b)

	assert.Contains(t, text, "Now: GPL-2.0-only, GPL-3.0-only (default\n      # policy, revision 2)\n"+
		"      # deny:\n"+
		"      #   licenses:\n"+
		"      #     - GPL-2.0-only\n"+
		"      #     - GPL-3.0-only\n")
	assert.Contains(t, text, "Values: {licenses: [text, ...]}. Now: not set, each\n      # tool uses its local value\n      # allow:\n")
	assert.Contains(t, text, "allow, report, fail. Now: allow (default policy, revision 2)\n      # unknown: allow\n")
	for _, line := range strings.Split(text, "\n") {
		assert.LessOrEqual(t, len(line), templateWidth, line)
	}
}

func TestMarshalPolicyTemplate_LicenseListsRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		spec *specv1.PolicySpec
		want string
	}{
		{"an empty deny list denies nothing", specWithLicenses(licenseList(), nil, 0), "      deny:\n        licenses: []\n"},
		{"an empty allow list turns the allow list off", specWithLicenses(nil, licenseList(), 0), "      allow:\n        licenses: []\n"},
		{"lists with entries", specWithLicenses(licenseList("AGPL-3.0-only"), licenseList("MIT", "Apache-2.0"),
			scangatev1.UnknownLicenseGate_UNKNOWN_LICENSE_GATE_FAIL),
			"      deny:\n        licenses:\n          - AGPL-3.0-only\n      allow:\n        licenses:\n          - MIT\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &policyv1.Policy{}
			p.SetBase(tt.spec)
			b, err := MarshalPolicyTemplate(p, "", defaultPolicy())
			require.NoError(t, err)
			assert.Contains(t, string(b), tt.want)

			got, err := UnmarshalPolicy(b)
			require.NoError(t, err)
			assert.True(t, proto.Equal(p, got), "got %v\n%s", got, b)
			assert.True(t, proto.Equal(p, PruneEmpty(got)), "a set list with no items is a value")
		})
	}
}

func TestUnmarshalPolicy_AnUncommentedListKeyAloneSetsNothing(t *testing.T) {
	for name, doc := range map[string]string{
		"the setting key":                   "      deny:\n",
		"the setting key and its list key":  "      deny:\n        licenses:\n        # - GPL-3.0-only\n",
		"the wrapper form with no list key": "      deny: {}\n",
		"the wrapper form with a null list": "      deny: {licenses: }\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := UnmarshalPolicy([]byte("base:\n  scan_gate:\n    licenses:\n" + doc))
			require.NoError(t, err)
			assert.False(t, got.GetBase().GetScanGate().GetLicenses().HasDeny(), "a key with no list must not empty the deny list")
			assert.False(t, PruneEmpty(got).(*policyv1.Policy).HasBase())
		})
	}
}

func TestUnmarshalPolicy_ListSettingForms(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{"the bare empty list", "deny: []", []string{}},
		{"the bare list", "deny: [GPL-3.0-only]", []string{"GPL-3.0-only"}},
		{"the wrapper form with an empty list", "deny: {licenses: []}", []string{}},
		{"the wrapper form", "deny:\n        licenses:\n          - GPL-3.0-only", []string{"GPL-3.0-only"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalPolicy([]byte("base:\n  scan_gate:\n    licenses:\n      " + tt.doc + "\n"))
			require.NoError(t, err)
			licenses := got.GetBase().GetScanGate().GetLicenses()
			require.True(t, licenses.HasDeny(), "a list sets the deny list")
			assert.Equal(t, tt.want, append([]string{}, licenses.GetDeny().GetLicenses()...))
		})
	}

	aliases := []struct {
		name string
		doc  string
		want []string
	}{
		{"an alias to a wrapper", "deny: &empty {licenses: []}\n      allow: *empty", []string{}},
		{"an alias to a bare list", "deny: &list [GPL-3.0-only]\n      allow: *list", []string{"GPL-3.0-only"}},
		{"an alias to the list of a wrapper", "deny: {licenses: &list [MIT]}\n      allow: {licenses: *list}", []string{"MIT"}},
	}
	for _, tt := range aliases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalPolicy([]byte("base:\n  scan_gate:\n    licenses:\n      " + tt.doc + "\n"))
			require.NoError(t, err)
			licenses := got.GetBase().GetScanGate().GetLicenses()
			require.True(t, licenses.HasDeny())
			require.True(t, licenses.HasAllow(), "the alias sets the allow list")
			assert.Equal(t, tt.want, append([]string{}, licenses.GetAllow().GetLicenses()...))
		})
	}

	_, err := UnmarshalPolicy([]byte("base:\n  scan_gate:\n    licenses:\n      deny: {licenses: [MIT], licences: [MIT]}\n"))
	assert.ErrorContains(t, err, "scan_gate.licenses.deny.licences: unknown field. Known fields: licenses")
	_, err = UnmarshalPolicy([]byte("base:\n  scan_gate:\n    licenses:\n      deny: MIT\n"))
	assert.ErrorContains(t, err, "expected a list")
}
