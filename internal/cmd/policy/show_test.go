package policy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	packageadmissionv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/packageadmission/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func TestRunShow(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	res, err := runShow(context.Background(), client, "platform-eng.acme.safedep.io")
	require.NoError(t, err)

	require.Len(t, res.Overrides, 2)
	assert.Equal(t, "ci", res.Overrides[0].Name, "the highest priority comes first")
	assert.Equal(t, "custom/environment=ci", res.Overrides[0].Selector)

	table := res.RenderTable()
	for _, want := range []string{
		"Policy          platform-eng.acme.safedep.io",
		`46 · published 2d ago by alice · "cooldown 5d for laptops"`,
		"Open changes    none",
		"211 (184 trust, 27 block) · 23 expire in 7d",
		"Default policy  revision 1",
		"  fail_on_attacks  default policy             fail",
		"Overrides (priority, highest first)",
		"payments",
	} {
		assert.Contains(t, table, want)
	}

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

func showResponse(def *specv1.PolicySpec, policy *policyv1.Policy) *policyv1.GetPolicyResponse {
	head := revision(4, "update package rule r_p0jz0vqmxa")
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

func TestShowRenderTable(t *testing.T) {
	noOverrides := &policyv1.Policy{}

	baseOnly := &policyv1.Policy{}
	baseOnly.SetBase(failOnSpec(scangatev1.Severity_SEVERITY_MEDIUM))
	baseOnly.SetBaseDescription("Every repository gates on medium findings")

	cooldownCI := override("ov_ci", "ci", 300, "custom/environment", "ci", scangatev1.Severity_SEVERITY_CRITICAL)
	cooldownCI.SetDescription("CI runners build release artifacts, so the gate fails on critical findings " +
		"only and the cooldown is longer than on laptops")
	cooldown := &packageadmissionv1.Cooldown{}
	cooldown.SetMinAge(durationpb.New(7 * 24 * time.Hour))
	admission := &packageadmissionv1.PackageAdmission{}
	admission.SetCooldown(cooldown)
	cooldownCI.GetSpec().SetPackageAdmission(admission)
	severalOverrides := &policyv1.Policy{}
	severalOverrides.SetBase(failOnSpec(scangatev1.Severity_SEVERITY_MEDIUM))
	severalOverrides.SetOverrides([]*policyv1.Override{
		override("ov_pay", "payments", 200, "custom/criticality", "high", scangatev1.Severity_SEVERITY_HIGH),
		cooldownCI,
		override("ov_lab", "lab", 100, "custom/environment", "lab", scangatev1.Severity_SEVERITY_LOW),
	})

	tests := []struct {
		name    string
		res     *policyv1.GetPolicyResponse
		width   int
		want    []string
		wantNot []string
	}{
		{
			name:  "long license list at 80 columns",
			res:   showResponse(defaultPolicySpec(), noOverrides),
			width: 80,
			want: []string{
				"Head revision   4 · published 14d ago by alice ·\n" +
					"                \"update package rule r_p0jz0vqmxa\"\n",
				"Package rules   1 (0 trust, 1 block) · safedep policy rule list\n",
				"Settings\n" +
					"SETTING            SOURCE          VALUE\n" +
					"scan_gate\n" +
					"  fail_on          default policy  high\n" +
					"  fail_on_attacks  default policy  fail\n" +
					"scan_gate.licenses\n" +
					"  deny             default policy  GPL-2.0, GPL-2.0-only, GPL-2.0-or-later,\n" +
					"                                   GPL-3.0, GPL-3.0-only, GPL-3.0-or-later,\n" +
					"                                   AGPL-3.0, AGPL-3.0-only, AGPL-3.0-or-later\n" +
					"  unknown          default policy  allow\n" +
					"See the settings document: safedep policy get\n",
			},
		},
		{
			name:  "long license list at 140 columns",
			res:   showResponse(defaultPolicySpec(), noOverrides),
			width: 140,
			want: []string{
				"Head revision   4 · published 14d ago by alice · \"update package rule r_p0jz0vqmxa\"\n",
				"  deny             default policy  GPL-2.0, GPL-2.0-only, GPL-2.0-or-later, GPL-3.0, GPL-3.0-only, " +
					"GPL-3.0-or-later, AGPL-3.0,\n" +
					"                                   AGPL-3.0-only, AGPL-3.0-or-later\n",
			},
		},
		{
			name:  "empty base and default policy",
			res:   showResponse(nil, noOverrides),
			width: 80,
			want: []string{
				"Settings\nThe policy sets no settings. Each tool uses its local values.\n" +
					"Set them with: safedep policy edit\n",
				"No policy overrides.",
			},
			wantNot: []string{"SETTING", "safedep policy get"},
		},
		{
			name:  "no overrides",
			res:   showResponse(defaultPolicySpec(), baseOnly),
			width: 80,
			want: []string{
				"Settings\nBase: Every repository gates on medium findings\nSETTING",
				"  fail_on          base            medium\n" +
					"  fail_on_attacks  default policy  fail\n",
				"No policy overrides. The policy base applies to every subject.",
			},
			wantNot: []string{"override \"", "An override sets its value"},
		},
		{
			name:  "several overrides",
			res:   showResponse(defaultPolicySpec(), severalOverrides),
			width: 80,
			want: []string{
				"SETTING            SOURCE                     VALUE\n" +
					"package_admission.cooldown\n" +
					"  min_age          -                          not set, each tool uses its local\n" +
					"                                              value\n" +
					"                   override \"ci\" (300)        7d\n" +
					"scan_gate\n" +
					"  fail_on          base                       medium\n" +
					"                   override \"ci\" (300)        critical\n" +
					"                   override \"payments\" (200)  high\n" +
					"                   override \"lab\" (100)       low\n" +
					"  fail_on_attacks  default policy             fail\n",
				"An override sets its value only for the subjects that its selector matches.\n" +
					"See the settings document: safedep policy get\n",
				"CI runners build",
			},
			wantNot: []string{"CI runners build release artifacts, so the gate"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewPolicyServiceClientMock(t)
			client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(tt.res, nil)
			res, err := runShow(context.Background(), client, "default-team.safedep-io.safedep.io")
			require.NoError(t, err)

			out := res.render(fixedNow, tt.width)
			for _, want := range tt.want {
				assert.Contains(t, out, want)
			}
			for _, notWant := range tt.wantNot {
				assert.NotContains(t, out, notWant)
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
	table := got.RenderTable()
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
