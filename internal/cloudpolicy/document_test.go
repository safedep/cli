package cloudpolicy

import (
	"strings"
	"testing"
	"time"

	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMarshalPolicy_WritesTheWalkthroughForm(t *testing.T) {
	header := CommentBlock([]string{"Policy of acme", "", "Saving creates a draft change."})
	b, err := MarshalPolicy(samplePolicy(), header)
	require.NoError(t, err)

	want := header + `base:
  package_admission:
    cooldown:
      min_age: 48h
  scan_gate:
    fail_on: high
overrides:
  - id: ov_2
    name: ci
    priority: 300
    selector:
      matchLabels: {custom/environment: ci}
    spec:
      package_admission:
        cooldown:
          min_age: 168h
  - id: ov_1
    name: payments
    description: Stricter gate for payment services (CUST-412)
    priority: 200
    selector:
      matchLabels: {custom/criticality: high}
    spec:
      package_admission:
        cooldown:
          min_age: 168h
      scan_gate:
        fail_on: medium
`
	assert.Equal(t, want, string(b))
}

func TestUnmarshalPolicy_RoundTrips(t *testing.T) {
	original := samplePolicy()
	b, err := MarshalPolicy(original, "")
	require.NoError(t, err)

	got, err := UnmarshalPolicy(b)
	require.NoError(t, err)
	assert.True(t, proto.Equal(original, got), "got %v", got)
}

func TestUnmarshalPolicy_AcceptsDaysAndFullEnumNames(t *testing.T) {
	got, err := UnmarshalPolicy([]byte(`
base:
  scan_gate:
    fail_on: SEVERITY_CRITICAL
    fail_on_attacks: report
  package_admission:
    cooldown: { min_age: 7d }
`))
	require.NoError(t, err)
	assert.Equal(t, scangatev1.Severity_SEVERITY_CRITICAL, got.GetBase().GetScanGate().GetFailOn())
	assert.Equal(t, scangatev1.AttackGate_ATTACK_GATE_REPORT, got.GetBase().GetScanGate().GetFailOnAttacks())
	assert.Equal(t, 7*24*time.Hour, got.GetBase().GetPackageAdmission().GetCooldown().GetMinAge().AsDuration())
}

func TestUnmarshalPolicy_Errors(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "unknown field names the known ones",
			doc:  "base:\n  scan_gates: {}\n",
			want: "line 2: base.scan_gates: unknown field. Known fields: package_admission, scan_gate",
		},
		{
			name: "unknown enum value lists the values",
			doc:  "base:\n  scan_gate:\n    fail_on: mediun\n",
			want: `line 3: base.scan_gate.fail_on: unknown value "mediun". Use one of: critical, high, medium, low, info`,
		},
		{
			name: "package rules are not in the document",
			doc:  "base:\n  core: {}\n",
			want: "base.core: package rules are not in this file",
		},
		{
			name: "bad duration",
			doc:  "base:\n  package_admission:\n    cooldown:\n      min_age: soon\n",
			want: "line 4: base.package_admission.cooldown.min_age: invalid duration",
		},
		{
			name: "bad priority",
			doc:  "overrides:\n  - name: x\n    priority: high\n",
			want: "line 3: overrides[0].priority: expected a positive whole number",
		},
		{
			name: "bad selector operator",
			doc:  "overrides:\n  - name: x\n    selector:\n      matchExpressions:\n        - {key: custom/a, operator: Maybe}\n",
			want: `unknown operator "Maybe"`,
		},
		{
			name: "duplicate field",
			doc:  "base_description: a\nbase_description: b\n",
			want: "line 2: base_description: the field is set twice",
		},
		{
			name: "empty file",
			doc:  "# only a comment\n",
			want: "the file is empty",
		},
		{
			name: "not YAML",
			doc:  "base: [\n",
			want: "the file is not valid YAML",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalPolicy([]byte(tt.doc))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestUnmarshalPolicy_KeepsTheIDThatTheUserLeaves(t *testing.T) {
	got, err := UnmarshalPolicy([]byte(`
overrides:
  - id: ov_1
    name: renamed
    priority: 1
    selector: {}
  - override_id: ov_2
    name: proto-name-also-works
    priority: 2
    selector: {}
  - name: new
    priority: 3
    selector: {}
`))
	require.NoError(t, err)
	require.Len(t, got.GetOverrides(), 3)
	assert.Equal(t, "ov_1", got.GetOverrides()[0].GetOverrideId(), "a rename keeps the id")
	assert.Equal(t, "ov_2", got.GetOverrides()[1].GetOverrideId())
	assert.Empty(t, got.GetOverrides()[2].GetOverrideId(), "a new override gets its id from the server")
}

func TestCheckKnownFields(t *testing.T) {
	require.NoError(t, CheckKnownFields(samplePolicy()))
	require.NoError(t, CheckKnownFields(nil))

	p := samplePolicy()
	// Field 99 of ScanGate is a field that a later contract can add.
	unknown := protowire.AppendTag(nil, 99, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 1)
	p.GetOverrides()[1].GetSpec().GetScanGate().ProtoReflect().SetUnknown(unknown)
	p.ProtoReflect().SetUnknown(unknown)

	err := CheckKnownFields(p)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownFields)
	assert.Contains(t, err.Error(), "the document root")
	assert.Contains(t, err.Error(), "overrides[1].spec.scan_gate")
}

func TestHasUserComments(t *testing.T) {
	header := CommentBlock([]string{"Policy of acme"})
	body := "base:\n  scan_gate:\n    fail_on: high\n"
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"header only", header + body, false},
		{"line comment", header + strings.Replace(body, "high", "high # CUST-412", 1), true},
		{"comment between keys", header + "# why\n" + body, true},
		{"changed header", "# my own header\n" + body, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, HasUserComments([]byte(tt.text), []byte(header)))
		})
	}
}

func TestPolicyMap(t *testing.T) {
	doc, err := PolicyMap(samplePolicy())
	require.NoError(t, err)
	m, ok := doc.(map[string]any)
	require.True(t, ok)
	base, ok := m["base"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"fail_on": "high"}, base["scan_gate"])
}

func TestOverrideExpiry_KeepsFractionalSeconds(t *testing.T) {
	p := samplePolicy()
	expires := time.Date(2027, 1, 26, 10, 30, 0, 123456789, time.UTC)
	p.GetOverrides()[0].SetExpiresAt(timestamppb.New(expires))
	p.GetOverrides()[1].SetExpiresAt(timestamppb.New(time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)))

	b, err := MarshalPolicy(p, "")
	require.NoError(t, err)
	assert.Contains(t, string(b), "expires_at: 2027-01-26T10:30:00.123456789Z")
	assert.Contains(t, string(b), "expires_at: 2027-02-01T00:00:00Z", "a zero fraction stays out")

	got, err := UnmarshalPolicy(b)
	require.NoError(t, err)
	assert.True(t, proto.Equal(p, got), "an edit with no change must not change the expiry")
}
