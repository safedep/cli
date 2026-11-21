package policy

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packageadmissionv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/packageadmission/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func TestRunShow(t *testing.T) {
	head := headResponse()
	head.GetHead().SetPublishedAt(timestamppb.New(fixedNow.Add(-49 * time.Hour)))
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(head, nil)

	res, err := runShow(context.Background(), client, "platform-eng.acme.safedep.io")
	require.NoError(t, err)

	require.Len(t, res.Overrides, 2)
	assert.Equal(t, "ci", res.Overrides[0].Name, "the highest priority comes first")
	assert.Equal(t, "custom/environment=ci", res.Overrides[0].Selector)

	assertLinesInOrder(t, res.render(fixedNow, 80),
		`Policy +platform-eng.acme.safedep.io`,
		`Head revision +46 · published 2d ago by alice · "cooldown 5d for laptops"`,
		`Open changes +none`,
		`Package rules +211 \(184 trust, 27 block\) · 23 expire in 7d ·`,
		`Default policy +revision 1`,
		`  fail_on_attacks +default policy +fail`,
		`Overrides \(priority, highest first\)`,
		`.*payments.*`,
	)

	b, err := res.RenderJSON()
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	assert.InDelta(t, 46, out["head_revision"], 0)
	assert.Equal(t, map[string]any{"scan_gate.fail_on": "high", "scan_gate.fail_on_attacks": "fail"}, out["default_policy"])
	policy, ok := out["policy"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, policy, "overrides")

	assert.Contains(t, res.RenderPlain(), "override\t300\tci\tcustom/environment=ci")
}

// riskyLicenses is the deny list of the SafeDep default policy.
var riskyLicenses = []string{
	"GPL-2.0", "GPL-2.0-only", "GPL-2.0-or-later",
	"GPL-3.0", "GPL-3.0-only", "GPL-3.0-or-later",
	"AGPL-3.0", "AGPL-3.0-only", "AGPL-3.0-or-later",
}

// longOverrideName has the 63 characters that the API allows for a name.
const longOverrideName = "payments-team-release-freeze-exception-for-the-q4-2026-rollout1"

// defaultPolicySpec is the SafeDep default policy at revision 2: the scan
// gate with the deny list of risky licenses.
func defaultPolicySpec() *specv1.PolicySpec {
	spec := failOnSpec(scangatev1.Severity_SEVERITY_HIGH)
	spec.GetScanGate().SetFailOnAttacks(scangatev1.AttackGate_ATTACK_GATE_FAIL)
	deny := &scangatev1.LicenseList{}
	deny.SetLicenses(riskyLicenses)
	licenses := &scangatev1.LicenseGate{}
	licenses.SetDeny(deny)
	licenses.SetUnknown(scangatev1.UnknownLicenseGate_UNKNOWN_LICENSE_GATE_ALLOW)
	spec.GetScanGate().SetLicenses(licenses)
	return spec
}

// showResponse is the reply of the example tenant: head revision 4, one
// block rule and the default policy at revision 2.
func showResponse(def *specv1.PolicySpec, policy *policyv1.Policy) *policyv1.GetPolicyResponse {
	head := revision(4, "update package rule r_p0jz0vqmxa")
	author := &messagescontroltowerv1.Actor{}
	author.SetId("abhisek@safedep.io")
	head.SetAuthor(author)
	head.SetPublishedAt(timestamppb.New(fixedNow.Add(-14 * 24 * time.Hour)))
	defaultPolicy := &policyv1.DefaultPolicy{}
	defaultPolicy.SetRevision(2)
	defaultPolicy.SetSpec(def)
	counts := &policyv1.PackageRuleCounts{}
	counts.SetBlockCount(1)
	res := &policyv1.GetPolicyResponse{}
	res.SetPolicy(policy)
	res.SetHeadRevision(4)
	res.SetHead(head)
	res.SetDefaultPolicy(defaultPolicy)
	res.SetPackageRuleCounts(counts)
	return res
}

func renderShow(t *testing.T, res *policyv1.GetPolicyResponse, width int) string {
	t.Helper()
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(res, nil)
	got, err := runShow(context.Background(), client, "default-team.safedep-io.safedep.io")
	require.NoError(t, err)
	return got.render(fixedNow, width)
}

// assertLinesInOrder checks that each pattern matches a whole line, each one
// below the line of the pattern before it.
func assertLinesInOrder(t *testing.T, out string, patterns ...string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	next := 0
	for _, pattern := range patterns {
		re := regexp.MustCompile("^" + pattern + "$")
		found := false
		for i := next; i < len(lines); i++ {
			if re.MatchString(lines[i]) {
				next, found = i+1, true
				break
			}
		}
		if !assert.True(t, found, "no line matches %q below line %d in:\n%s", pattern, next, out) {
			return
		}
	}
}

func TestShowRenderTable_Example(t *testing.T) {
	out := renderShow(t, showResponse(defaultPolicySpec(), &policyv1.Policy{}), 80)

	settingsEnd := strings.Index(out, "\n\nOverrides")
	require.Positive(t, settingsEnd)
	assert.Equal(t, `Policy          default-team.safedep-io.safedep.io
Head revision   4 · published 14d ago by abhisek@safedep.io ·
                "update package rule r_p0jz0vqmxa"
Open changes    none
Package rules   1 (0 trust, 1 block) · safedep policy rule list
Default policy  revision 2

Settings
SETTING            SOURCE          VALUE
package_admission.cooldown
  min_age          -               not set, each tool uses its local value
scan_gate
  fail_on          default policy  high
  fail_on_attacks  default policy  fail
scan_gate.licenses
  deny             default policy  GPL-2.0, GPL-2.0-only, GPL-2.0-or-later,
                                   GPL-3.0, GPL-3.0-only, GPL-3.0-or-later,
                                   AGPL-3.0, AGPL-3.0-only, AGPL-3.0-or-later
  allow            -               not set, each tool uses its local value
  unknown          default policy  allow
See the settings document: safedep policy get`, out[:settingsEnd])
	assert.Contains(t, out, "No policy overrides. The policy base applies to every subject.")
}

func TestShowRenderTable(t *testing.T) {
	baseOnly := &policyv1.Policy{}
	baseOnly.SetBase(failOnSpec(scangatev1.Severity_SEVERITY_MEDIUM))
	baseOnly.SetBaseDescription("Every repository gates on medium findings")

	ci := override("ov_ci", "ci", 300, "custom/environment", "ci", scangatev1.Severity_SEVERITY_CRITICAL)
	ci.SetDescription("CI runners build release artifacts, so the gate fails on critical findings " +
		"only and the cooldown is longer than on laptops")
	cooldown := &packageadmissionv1.Cooldown{}
	cooldown.SetMinAge(durationpb.New(7 * 24 * time.Hour))
	admission := &packageadmissionv1.PackageAdmission{}
	admission.SetCooldown(cooldown)
	ci.GetSpec().SetPackageAdmission(admission)
	lab := override("ov_lab", "lab", 100, "custom/environment", "lab", scangatev1.Severity_SEVERITY_LOW)
	lab.SetExpiresAt(timestamppb.New(fixedNow.Add(-time.Hour)))
	payments := override("ov_pay", "payments", 200, "custom/criticality", "high", scangatev1.Severity_SEVERITY_HIGH)
	payments.SetExpiresAt(timestamppb.New(fixedNow.Add(time.Hour)))
	severalOverrides := &policyv1.Policy{}
	severalOverrides.SetBase(failOnSpec(scangatev1.Severity_SEVERITY_MEDIUM))
	severalOverrides.SetOverrides([]*policyv1.Override{payments, ci, lab})

	longName := &policyv1.Policy{}
	longName.SetOverrides([]*policyv1.Override{
		override("ov_long", longOverrideName, 1000000, "custom/team", "payments", scangatev1.Severity_SEVERITY_CRITICAL),
		override("ov_ci", "ci", 300, "custom/environment", "ci", scangatev1.Severity_SEVERITY_LOW),
	})

	escaped := &policyv1.Policy{}
	escaped.SetBaseDescription("\x1b[31mred\nline")
	escaped.SetOverrides([]*policyv1.Override{
		override("ov_evil", "evil\x1b[2J", 100, "custom/environment", "ci", scangatev1.Severity_SEVERITY_LOW),
	})

	allowSpec := defaultPolicySpec()
	allow := &scangatev1.LicenseList{}
	allow.SetLicenses([]string{"MIT", "ISC", "Apache-2.0", "GPL-2.0-only WITH Classpath-exception-2.0"})
	allowSpec.GetScanGate().GetLicenses().SetAllow(allow)

	unknownBase := failOnSpec(scangatev1.Severity_SEVERITY_MEDIUM)
	unknownField := protowire.AppendTag(nil, 99, protowire.VarintType)
	unknownBase.GetScanGate().ProtoReflect().SetUnknown(protowire.AppendVarint(unknownField, 1))
	unknownPolicy := &policyv1.Policy{}
	unknownPolicy.SetBase(unknownBase)

	unknownNote := "This safedep CLI does not know some settings of this policy."
	tests := []struct {
		name string
		res  *policyv1.GetPolicyResponse
		// width 0 is a stdout that is not a terminal.
		width int
		// overflows is true when the width is below the narrowest value
		// column, so lines can pass the width.
		overflows bool
		// tableOverflows checks the width only above the overrides table.
		tableOverflows bool
		lines          []string
		want           []string
		wantNot        []string
	}{
		{
			name:  "long license list at 140 columns",
			res:   showResponse(defaultPolicySpec(), &policyv1.Policy{}),
			width: 140,
			lines: []string{
				`Head revision   4 · published 14d ago by abhisek@safedep.io · "update package rule r_p0jz0vqmxa"`,
				`  deny +default policy +GPL-2.0, GPL-2.0-only, GPL-2.0-or-later, GPL-3.0, GPL-3.0-only, GPL-3.0-or-later, AGPL-3.0,`,
				` +AGPL-3.0-only, AGPL-3.0-or-later`,
			},
		},
		{
			name:  "no wrap when stdout is not a terminal",
			res:   showResponse(defaultPolicySpec(), longName),
			width: 0,
			lines: []string{
				`Head revision   4 · published 14d ago by abhisek@safedep.io · "update package rule r_p0jz0vqmxa"`,
				` +override "` + longOverrideName + `" \(1000000\) +critical`,
				`  deny +default policy +` + strings.Join(riskyLicenses, ", "),
			},
		},
		{
			name:  "empty base and default policy",
			res:   showResponse(nil, &policyv1.Policy{}),
			width: 80,
			lines: []string{
				`  min_age +- +not set, each tool uses its local value`,
				`  fail_on +- +not set, each tool uses its local value`,
				`  fail_on_attacks +- +not set, each tool uses its local value`,
				`  deny +- +not set, each tool uses its local value`,
				`  allow +- +not set, each tool uses its local value`,
				`  unknown +- +not set, each tool uses its local value`,
			},
			wantNot: []string{"default policy  ", "base  "},
		},
		{
			name:  "no overrides",
			res:   showResponse(defaultPolicySpec(), baseOnly),
			width: 80,
			lines: []string{
				`Settings`,
				`Base: Every repository gates on medium findings`,
				`SETTING +SOURCE +VALUE`,
				`  fail_on +base +medium`,
				`  fail_on_attacks +default policy +fail`,
			},
			want:    []string{"No policy overrides. The policy base applies to every subject."},
			wantNot: []string{`override "`, "An override sets its value", unknownNote},
		},
		{
			name:  "several overrides in priority order with an expired one",
			res:   showResponse(defaultPolicySpec(), severalOverrides),
			width: 80,
			lines: []string{
				`  min_age +- +not set, each tool uses its local`,
				` +value`,
				` +override "ci" \(300\) +7d`,
				`  fail_on +base +medium`,
				` +override "ci" \(300\) +critical`,
				` +override "payments" \(200\) +high`,
				`    override "lab" \(100\), expired`,
				` +low`,
				`  fail_on_attacks +default policy +fail`,
				`An override sets its value only for the subjects that its selector matches.`,
			},
			want:    []string{"CI runners build"},
			wantNot: []string{"CI runners build release artifacts, so the gate", `"payments" (200), expired`},
		},
		{
			name:  "override name of 63 characters at 80 columns",
			res:   showResponse(defaultPolicySpec(), longName),
			width: 80,
			// A name has no space to wrap at, and a dry/tui table has no
			// width option, so the NAME column passes the width.
			tableOverflows: true,
			lines: []string{
				`  fail_on +default policy +high`,
				`    override "` + longOverrideName + `"`,
				`    \(1000000\)`,
				` +critical`,
				` +override "ci" \(300\) +low`,
				`  fail_on_attacks +default policy +fail`,
			},
		},
		{
			name:  "server text is escaped",
			res:   showResponse(nil, escaped),
			width: 80,
			lines: []string{
				`Base: \\x1b\[31mred\\nline`,
				`    override "evil\\x1b\[2J" \(100\)`,
				` +low`,
			},
			wantNot: []string{"\x1b", "red\nline"},
		},
		{
			name:  "list item with spaces stays whole at 80 columns",
			res:   showResponse(allowSpec, &policyv1.Policy{}),
			width: 80,
			lines: []string{
				`  allow +default policy +MIT, ISC, Apache-2.0,`,
				` +GPL-2.0-only WITH Classpath-exception-2.0`,
			},
		},
		{
			name:      "value column keeps its minimum width on a narrow terminal",
			res:       showResponse(nil, &policyv1.Policy{}),
			width:     30,
			overflows: true,
			lines: []string{
				`  min_age +- +not set, each tool`,
				` +uses its local value`,
			},
		},
		{
			name:  "settings that this CLI does not know",
			res:   showResponse(defaultPolicySpec(), unknownPolicy),
			width: 80,
			lines: []string{
				`  fail_on +base +medium`,
				`This safedep CLI does not know some settings of this policy.`,
				`Update the CLI to the latest release to see them.`,
				`See the settings document: safedep policy get`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderShow(t, tt.res, tt.width)
			assertLinesInOrder(t, out, tt.lines...)
			for _, want := range tt.want {
				assert.Contains(t, out, want)
			}
			for _, notWant := range tt.wantNot {
				assert.NotContains(t, out, notWant)
			}
			if tt.width <= 0 || tt.overflows {
				return
			}
			if tt.tableOverflows {
				settings, _, found := strings.Cut(out, "\n\nOverrides")
				require.True(t, found)
				out = settings
			}
			for _, line := range strings.Split(out, "\n") {
				assert.LessOrEqual(t, lipgloss.Width(line), tt.width, "line %q", line)
			}
		})
	}
}

func TestRunShow_NeverPublished(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.GetPolicyResponse{}
	res.SetOpenChangeCount(2)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(res, nil)

	got, err := runShow(context.Background(), client, "")
	require.NoError(t, err)
	table := got.render(fixedNow, 80)
	assert.Contains(t, table, "none: the tenant has not published a policy yet")
	assert.Contains(t, table, "2 · safedep policy change list --state open")
	assert.Contains(t, table, "No policy overrides")
}

func TestRunGet(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	res, err := runGet(context.Background(), client, getInput{}, "acme")
	require.NoError(t, err)
	assert.Equal(t, uint64(46), res.Revision)
	assert.Contains(t, res.RenderPlain(), "# Policy settings of acme · revision 46\n")
	assert.Contains(t, res.RenderTable(), "overrides:\n  - id: ov_pay\n    name: payments")
	b, err := res.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"revision": 46`)
}

func TestRunGet_AtRevision(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	rev := &policyv1.GetPolicyRevisionResponse{}
	rev.SetRevision(revision(40, "old"))
	rev.SetPolicy(headPolicy())
	client.EXPECT().GetPolicyRevision(mock.Anything, mock.MatchedBy(func(r *policyv1.GetPolicyRevisionRequest) bool {
		return r.GetRevision() == 40
	})).Return(rev, nil)

	res, err := runGet(context.Background(), client, getInput{Revision: 40}, "")
	require.NoError(t, err)
	assert.Contains(t, res.YAML, "# Policy settings of this tenant · revision 40")
}
