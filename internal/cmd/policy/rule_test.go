package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

func TestValidateRuleCreate(t *testing.T) {
	block := ruleCreateInput{Block: true, Selector: "pkg:npm/evil", Reason: "IR-2291"}
	trust := ruleCreateInput{Trust: true, Selector: "pkg:pypi/foo@1.0.3", Reason: "FP", Scopes: []string{"malware"}, Expires: "30d"}
	tests := []struct {
		name   string
		mutate func(*ruleCreateInput)
		base   ruleCreateInput
		want   string
	}{
		{"block", func(*ruleCreateInput) {}, block, ""},
		{"trust", func(*ruleCreateInput) {}, trust, ""},
		{"neither", func(in *ruleCreateInput) { in.Block = false }, block, "give one of --block or --trust"},
		{"both", func(in *ruleCreateInput) { in.Trust = true }, block, "give one of --block or --trust"},
		{"no selector", func(in *ruleCreateInput) { in.Selector = "" }, block, "give the PURL"},
		{"selector and file", func(in *ruleCreateInput) { in.File = "x" }, block, "not both"},
		{"trust from file", func(in *ruleCreateInput) { in.Selector, in.File = "", "x" }, trust, "only block entries can come from a file"},
		{"vers with file", func(in *ruleCreateInput) { in.Selector, in.File, in.Vers = "", "x", ">=1" }, block, "--vers applies to one PURL"},
		{"no reason", func(in *ruleCreateInput) { in.Reason = " " }, block, "--reason is required"},
		{"trust without scope", func(in *ruleCreateInput) { in.Scopes = nil }, trust, "a trust entry needs --scope"},
		{"trust without expiry", func(in *ruleCreateInput) { in.Expires = "" }, trust, "a trust entry needs --expires"},
		{"block with scope", func(in *ruleCreateInput) { in.Scopes = []string{"malware"} }, block, "a block entry has no scope"},
		{"unknown scope", func(in *ruleCreateInput) { in.Scopes = []string{"secrets"} }, trust, `unknown trust scope "secrets"`},
		{"license", func(in *ruleCreateInput) { in.Scopes = []string{"license"} }, trust, ""},
		{"named vulnerability needs ids", func(in *ruleCreateInput) { in.Scopes = []string{"named-vulnerability"} }, trust,
			`unknown trust scope "named-vulnerability": allowed values are malware, cooldown, vulnerability, license`},
		{"waived vulnerabilities", func(in *ruleCreateInput) {
			in.Scopes, in.Vulnerabilities = []string{"vulnerability"}, []string{"CVE-2024-1234", "GHSA-jfh8-c2jp-5v3q"}
		}, trust, ""},
		{"waived vulnerabilities with another scope", func(in *ruleCreateInput) {
			in.Scopes, in.Vulnerabilities = []string{"malware"}, []string{"CVE-2024-1234"}
		}, trust, "--vulnerability needs --scope vulnerability"},
		{"waived vulnerabilities with a second scope", func(in *ruleCreateInput) {
			in.Scopes, in.Vulnerabilities = []string{"vulnerability", "license"}, []string{"CVE-2024-1234"}
		}, trust, "--vulnerability needs --scope vulnerability as the only scope"},
		{"waived vulnerabilities with all", func(in *ruleCreateInput) {
			in.Scopes, in.Vulnerabilities = []string{"all"}, []string{"CVE-2024-1234"}
		}, trust, "--vulnerability needs --scope vulnerability as the only scope"},
		{"waived vulnerabilities on a block", func(in *ruleCreateInput) { in.Vulnerabilities = []string{"CVE-2024-1234"} }, block,
			"a block entry waives no vulnerability"},
		{"a waived vulnerability twice", func(in *ruleCreateInput) {
			in.Scopes = []string{"vulnerability"}
			in.Vulnerabilities = []string{"CVE-2024-1234", "CVE-2024-1234:component_not_present"}
		}, trust, "--vulnerability CVE-2024-1234 is given more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.base
			tt.mutate(&in)
			err := validateRuleCreate(in)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

// allScopes is what --scope all expands to. It leaves out
// NAMED_VULNERABILITY, because that scope needs vulnerability ids.
var allScopes = []corev1.TrustScope{
	corev1.TrustScope_TRUST_SCOPE_MALWARE,
	corev1.TrustScope_TRUST_SCOPE_COOLDOWN,
	corev1.TrustScope_TRUST_SCOPE_VULNERABILITY,
	corev1.TrustScope_TRUST_SCOPE_LICENSE,
}

func TestExpandScopes_AllIsEveryKnownScope(t *testing.T) {
	all, err := expandScopes([]string{"all"})
	require.NoError(t, err)
	assert.Equal(t, allScopes, all)
	assert.NotContains(t, all, corev1.TrustScope_TRUST_SCOPE_NAMED_VULNERABILITY)
	for _, s := range all {
		assert.NotEqual(t, corev1.TrustScope_TRUST_SCOPE_UNSPECIFIED, s)
	}

	some, err := expandScopes([]string{"malware", "COOLDOWN", "malware"})
	require.NoError(t, err)
	assert.Equal(t, []corev1.TrustScope{corev1.TrustScope_TRUST_SCOPE_MALWARE, corev1.TrustScope_TRUST_SCOPE_COOLDOWN}, some)
}

func TestBuildSelector(t *testing.T) {
	block, trust := corev1.Effect_EFFECT_BLOCK, corev1.Effect_EFFECT_TRUST
	tests := []struct {
		purl, vers string
		effect     corev1.Effect
		wantVers   string
		wantErr    string
	}{
		{purl: "pkg:npm/evil", vers: ">=1.2.0", effect: block, wantVers: "vers:npm/>=1.2.0"},
		{purl: "pkg:npm/evil", vers: "vers:npm/>=1.2.0|<2.0.0", effect: block, wantVers: "vers:npm/>=1.2.0|<2.0.0"},
		{purl: "pkg:npm/@acme/*", effect: block},
		{purl: "pkg:maven/org.apache.*", effect: block},
		{purl: "pkg:nuget/Microsoft.*", effect: block},
		{purl: "pkg:npm/*", effect: block},
		{purl: "pkg:pypi/foo@1.0.3", effect: trust},
		{purl: "pkg:npm/@acme/foo", vers: ">=1", effect: trust, wantVers: "vers:npm/>=1"},
		{purl: "pkg:npm/@acme/*", effect: trust, wantErr: "a trust entry selects one package"},
		{purl: "pkg:npm/ac*", effect: block, wantErr: "a prefix has one '*'"},
		{purl: "pkg:npm/*/x", effect: block, wantErr: "a prefix has one '*'"},
		{purl: "pkg:npm/x@1.*", effect: block, wantErr: "a prefix has no version"},
		{purl: "pkg:npm/x?a=1/*", effect: block, wantErr: "a prefix has one '*'"},
		{purl: "pkg:npm/%2A", effect: block, wantErr: "do not encode '*'"},
		{purl: "npm/evil", effect: block, wantErr: "write a PURL"},
		{purl: "pkg:npm", effect: block, wantErr: "a PURL has a type and a name"},
		{purl: "pkg:", effect: block, wantErr: "write a PURL"},
		{purl: "pkg:npm//x", effect: block, wantErr: "a PURL has a type and a name"},
		{purl: "pkg:pypi/foo@1.0.3", vers: ">=1", effect: trust, wantErr: "not both"},
	}
	for _, tt := range tests {
		t.Run(tt.purl+" "+tt.vers, func(t *testing.T) {
			got, err := buildSelector(tt.purl, tt.vers, tt.effect)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.purl, got.GetPurl())
			assert.Equal(t, tt.wantVers, got.GetVers())
		})
	}
}

func TestParseExpiry(t *testing.T) {
	got, err := parseExpiry("30d", fixedNow)
	require.NoError(t, err)
	assert.Equal(t, fixedNow.Add(30*24*time.Hour), got)

	got, err = parseExpiry("2027-01-26", fixedNow)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2027, 1, 26, 0, 0, 0, 0, time.UTC), got)

	_, err = parseExpiry("2026-01-01", fixedNow)
	assert.ErrorContains(t, err, "in the past")
	_, err = parseExpiry("0s", fixedNow)
	assert.ErrorContains(t, err, "after now")
	_, err = parseExpiry("soon", fixedNow)
	assert.ErrorContains(t, err, "give a duration")
}

func TestBuildRuleCreates_Trust(t *testing.T) {
	in := ruleCreateInput{
		Trust: true, Selector: "pkg:pypi/foo@1.0.3", Scopes: []string{"malware"},
		Reason: "False positive, S-88", Expires: "30d", RequestedBy: "data-eng", Ticket: "S-88",
	}
	creates, err := buildRuleCreates(in, nil, fixedNow)
	require.NoError(t, err)
	require.Len(t, creates, 1)
	rule := creates[0].GetRule()
	assert.Equal(t, corev1.Effect_EFFECT_TRUST, rule.GetEffect())
	assert.Equal(t, []corev1.TrustScope{corev1.TrustScope_TRUST_SCOPE_MALWARE}, rule.GetScopes())
	assert.Equal(t, fixedNow.Add(30*24*time.Hour), rule.GetExpiresAt().AsTime())
	assert.Empty(t, rule.GetRuleId(), "the server sets the rule id")
	assert.Equal(t, "data-eng", creates[0].GetRequestedBy())
	assert.Equal(t, "S-88", creates[0].GetTicket())
}

func TestBuildRuleCreates_BulkBlockFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.txt")
	require.NoError(t, os.WriteFile(path, []byte("# incident IR-2291\npkg:npm/evil >=1.2.0\n\npkg:npm/@evil/*\n"), 0o600))

	creates, err := buildRuleCreates(ruleCreateInput{Block: true, File: path, Reason: "IR-2291"}, nil, fixedNow)
	require.NoError(t, err)
	require.Len(t, creates, 2)
	assert.Equal(t, "vers:npm/>=1.2.0", creates[0].GetRule().GetSelector().GetVers())
	assert.Equal(t, "pkg:npm/@evil/*", creates[1].GetRule().GetSelector().GetPurl())
	assert.False(t, creates[0].GetRule().HasExpiresAt())

	creates, err = buildRuleCreates(ruleCreateInput{Block: true, File: "-", Reason: "x"}, strings.NewReader("pkg:npm/a\n"), fixedNow)
	require.NoError(t, err)
	assert.Len(t, creates, 1, "- reads stdin")
}

func TestBuildRuleCreates_FileErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(text), 0o600))
		return p
	}
	var many strings.Builder
	for i := 0; i <= maxBulkRules; i++ {
		fmt.Fprintf(&many, "pkg:npm/p%d\n", i)
	}
	tests := []struct {
		name, path, want string
	}{
		{"bad line", write("bad.txt", "pkg:npm/ok\npkg:npm/ac*\n"), "bad.txt line 2: selector"},
		{"too many values", write("vals.txt", "pkg:npm/a >=1 extra\n"), "write a PURL and an optional range"},
		{"empty", write("empty.txt", "# nothing\n"), "has no selectors"},
		{"too many", write("many.txt", many.String()), "more than 1000 selectors"},
		{"missing", filepath.Join(dir, "missing.txt"), "Check the path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRuleCreates(ruleCreateInput{Block: true, File: tt.path, Reason: "x"}, nil, fixedNow)
			require.Error(t, err)
			msg := err.Error()
			if ue, ok := usefulerror.AsUsefulError(err); ok {
				msg += " " + ue.HumanError() + " " + ue.Help()
			}
			assert.Contains(t, msg, tt.want)
		})
	}
}

func ruleWithAttributes(id string, effect corev1.Effect, overrideID string) *policyv1.PackageRuleWithAttributes {
	sel := &corev1.PackageSelector{}
	sel.SetPurl("pkg:npm/leftpad")
	rule := &corev1.PackageRule{}
	rule.SetRuleId(id)
	rule.SetSelector(sel)
	rule.SetEffect(effect)
	rule.SetReason("FP, S-71")
	if effect == corev1.Effect_EFFECT_TRUST {
		rule.SetScopes([]corev1.TrustScope{corev1.TrustScope_TRUST_SCOPE_MALWARE})
		rule.SetExpiresAt(timestamppb.New(time.Now().Add(49 * time.Hour)))
	}
	r := &policyv1.PackageRuleWithAttributes{}
	r.SetRule(rule)
	r.SetOverrideId(overrideID)
	r.SetRequestedBy("web")
	r.SetAddedRevision(40)
	return r
}

func TestRunRuleCreate_AttachesToAnOverride(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	res := &policyv1.CreatePackageRulesResponse{}
	res.SetRules([]*policyv1.PackageRuleWithAttributes{ruleWithAttributes("r_9c1", corev1.Effect_EFFECT_BLOCK, "ov_pay")})
	res.SetRevision(revision(48, "block"))
	client.EXPECT().CreatePackageRules(mock.Anything, mock.MatchedBy(func(r *policyv1.CreatePackageRulesRequest) bool {
		return len(r.GetRules()) == 1 && r.GetRules()[0].GetOverrideId() == "ov_pay" && r.GetMessage() == "IR-2291"
	})).Return(res, nil)

	in := ruleCreateInput{Block: true, Selector: "pkg:npm/evil", Reason: "x", Override: "payments", Message: "IR-2291"}
	creates, err := buildRuleCreates(in, nil, fixedNow)
	require.NoError(t, err)
	got, err := runRuleCreate(context.Background(), client, in, creates)
	require.NoError(t, err)
	assert.Equal(t, uint64(48), got.Revision)
}

func TestRunRuleCreate_PermissionDenied(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().CreatePackageRules(mock.Anything, mock.Anything).Return(nil, status.Error(codes.PermissionDenied, "member"))
	in := ruleCreateInput{Block: true, Selector: "pkg:npm/evil", Reason: "x"}
	creates, err := buildRuleCreates(in, nil, fixedNow)
	require.NoError(t, err)
	_, err = runRuleCreate(context.Background(), client, in, creates)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "Your role cannot change package rules", ue.HumanError())
}

func TestRuleListFilters(t *testing.T) {
	in := &ruleListInput{Effect: "trust", Scope: "malware", SelectorPrefix: "pkg:npm/", Expiring: 7 * 24 * time.Hour, AtRevision: 40}
	f, err := ruleListFilters(in, "")
	require.NoError(t, err)
	assert.Equal(t, corev1.Effect_EFFECT_TRUST, f.GetEffect())
	assert.Equal(t, corev1.TrustScope_TRUST_SCOPE_MALWARE, f.GetScope())
	assert.Equal(t, "pkg:npm/", f.GetSelectorPrefix())
	assert.Equal(t, 7*24*time.Hour, f.GetExpiringWithin().AsDuration())
	assert.Equal(t, uint64(40), f.GetAtRevision())
	assert.False(t, f.HasOverrideId())

	f, err = ruleListFilters(&ruleListInput{Base: true}, "")
	require.NoError(t, err)
	assert.True(t, f.HasOverrideId(), "--base sends an empty override id")
	assert.Empty(t, f.GetOverrideId())

	f, err = ruleListFilters(&ruleListInput{}, "")
	require.NoError(t, err)
	assert.Nil(t, f)

	_, err = ruleListFilters(&ruleListInput{Base: true, Override: "x"}, "")
	assert.ErrorContains(t, err, "--override and --base")
	_, err = ruleListFilters(&ruleListInput{Effect: "allow"}, "")
	assert.ErrorContains(t, err, `unknown --effect value "allow"`)
}

func TestRunRuleList_NamesTheOverride(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.ListPackageRulesResponse{}
	res.SetRules([]*policyv1.PackageRuleWithAttributes{
		ruleWithAttributes("r_77e", corev1.Effect_EFFECT_TRUST, "ov_pay"),
		ruleWithAttributes("r_80b", corev1.Effect_EFFECT_BLOCK, ""),
	})
	client.EXPECT().ListPackageRules(mock.Anything, mock.Anything).Return(res, nil)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	got, err := runRuleList(context.Background(), client, &ruleListInput{})
	require.NoError(t, err)
	require.Len(t, got.Rules, 2)
	assert.Equal(t, `override "payments"`, got.Rules[0].AttachesTo)
	assert.Equal(t, "base", got.Rules[1].AttachesTo)
	assert.Equal(t, []string{"malware"}, got.Rules[0].Scopes)

	table := got.RenderTable()
	assert.Contains(t, table, "r_77e")
	assert.Contains(t, table, "in 2d")
	assert.Contains(t, table, "never")
	assert.Contains(t, got.RenderPlain(), "r_80b\tpkg:npm/leftpad\t\tblock\t\tFP, S-71\tweb\t\t")
}

func TestRunRuleShow(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.GetPackageRuleResponse{}
	res.SetRule(ruleWithAttributes("r_9c1", corev1.Effect_EFFECT_BLOCK, ""))
	client.EXPECT().GetPackageRule(mock.Anything, mock.Anything).Return(res, nil)

	got, err := runRuleShow(context.Background(), client, "r_9c1")
	require.NoError(t, err)
	table := got.RenderTable()
	assert.Contains(t, table, "Rule          r_9c1")
	assert.Contains(t, table, "added in revision 40 · active")

	updated := ruleWithAttributes("r_9c1", corev1.Effect_EFFECT_BLOCK, "")
	updated.SetUpdatedRevision(43)
	res.SetRule(updated)
	got, err = runRuleShow(context.Background(), client, "r_9c1")
	require.NoError(t, err)
	assert.Contains(t, got.RenderTable(), "added in revision 40, updated in revision 43 · active")
	assert.Contains(t, got.RenderPlain(), "updated_revision\t43")

	missing := mocks.NewPolicyServiceClientMock(t)
	missing.EXPECT().GetPackageRule(mock.Anything, mock.Anything).Return(nil, status.Error(codes.NotFound, "x"))
	_, err = runRuleShow(context.Background(), missing, "r_x")
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "Package rule r_x does not exist", ue.HumanError())
}

func TestBuildRuleUpdate(t *testing.T) {
	changed := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, c := range names {
				if c == n {
					return true
				}
			}
			return false
		}
	}
	update, err := buildRuleUpdate("r_77e", ruleUpdateInput{Expires: "90d", Reason: "renewed", changed: changed("expires", "reason")}, fixedNow)
	require.NoError(t, err)
	assert.Equal(t, fixedNow.Add(90*24*time.Hour), update.GetExpiresAt().AsTime())
	assert.Equal(t, "renewed", update.GetReason())
	assert.False(t, update.HasTicket())

	update, err = buildRuleUpdate("r_77e", ruleUpdateInput{changed: changed("ticket")}, fixedNow)
	require.NoError(t, err)
	assert.True(t, update.HasTicket(), "an empty --ticket clears the ticket")

	_, err = buildRuleUpdate("r_77e", ruleUpdateInput{}, fixedNow)
	assert.ErrorContains(t, err, "give at least one of")
	_, err = buildRuleUpdate("r_77e", ruleUpdateInput{changed: changed("reason")}, fixedNow)
	assert.ErrorContains(t, err, "--reason must not be empty")
}

func TestRunRuleUpdateAndDelete(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	upd := &policyv1.UpdatePackageRuleResponse{}
	upd.SetRevision(revision(50, ""))
	client.EXPECT().UpdatePackageRule(mock.Anything, mock.MatchedBy(func(r *policyv1.UpdatePackageRuleRequest) bool {
		return r.GetUpdate().GetRuleId() == "r_77e" && r.GetMessage() == "m"
	})).Return(upd, nil)
	update := &policyv1.PackageRuleUpdate{}
	update.SetRuleId("r_77e")
	res, err := runRuleUpdate(context.Background(), client, update, "m", fixedNow)
	require.NoError(t, err)
	assert.Equal(t, uint64(50), res.GetRevision().GetRevision())

	del := &policyv1.DeletePackageRuleResponse{}
	del.SetRevision(revision(51, ""))
	client.EXPECT().DeletePackageRule(mock.Anything, mock.MatchedBy(func(r *policyv1.DeletePackageRuleRequest) bool {
		return r.GetDelete().GetRuleId() == "r_80b" && r.GetDelete().GetReason() == "no longer needed"
	})).Return(del, nil)
	rev, err := runRuleDelete(context.Background(), client, "r_80b", "no longer needed", "")
	require.NoError(t, err)
	assert.Equal(t, uint64(51), rev)
}

func TestExpandScopes_AllMixedWithANamedScope(t *testing.T) {
	for _, in := range [][]string{{"malware", "all"}, {"all", "cooldown"}, {"ALL"}} {
		got, err := expandScopes(in)
		require.NoError(t, err)
		assert.Equal(t, allScopes, got, "%v", in)
	}
}

func TestRuleCreate_TrustExpiryLimit(t *testing.T) {
	in := ruleCreateInput{Trust: true, Selector: "pkg:pypi/foo@1.0.3", Scopes: []string{"malware"}, Reason: "FP", Expires: "90d"}
	_, err := buildRuleCreates(in, nil, fixedNow)
	require.NoError(t, err)

	in.Expires = "91d"
	connect := func() (policyv1grpc.PolicyServiceClient, error) {
		require.Fail(t, "createRules connected to the server before it validated the request")
		return nil, nil
	}
	err = createRules(context.Background(), connect, in, nil, func() time.Time { return fixedNow })
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Contains(t, ue.HumanError(), "a trust entry expires at most 90 days from now")

	block := ruleCreateInput{Block: true, Selector: "pkg:npm/evil", Reason: "x", Expires: "365d"}
	_, err = buildRuleCreates(block, nil, fixedNow)
	require.NoError(t, err, "a block entry has no expiry limit")
}

func TestAnnounceRuleCreate_WarnsAboutAWideSelector(t *testing.T) {
	stderr := captureStderr(t)
	in := ruleCreateInput{Block: true, Selector: "pkg:npm/*", Reason: "x"}
	creates, err := buildRuleCreates(in, nil, fixedNow)
	require.NoError(t, err)
	announceRuleCreate(in, creates)
	assert.Contains(t, stderr.String(), "Selector pkg:npm/* blocks every package of its ecosystem")

	stderr.Reset()
	in.Selector = "pkg:npm/@acme/*"
	creates, err = buildRuleCreates(in, nil, fixedNow)
	require.NoError(t, err)
	announceRuleCreate(in, creates)
	assert.NotContains(t, stderr.String(), "every package of its ecosystem")
}

func TestRunRuleUpdate_TrustExpiryLimit(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	res := &policyv1.GetPackageRuleResponse{}
	res.SetRule(ruleWithAttributes("r_77e", corev1.Effect_EFFECT_TRUST, ""))
	client.EXPECT().GetPackageRule(mock.Anything, mock.Anything).Return(res, nil)

	update := &policyv1.PackageRuleUpdate{}
	update.SetRuleId("r_77e")
	update.SetExpiresAt(timestamppb.New(fixedNow.Add(120 * 24 * time.Hour)))
	_, err := runRuleUpdate(context.Background(), client, update, "", fixedNow)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Contains(t, ue.HumanError(), "a trust entry expires at most 90 days from now")
}

func TestRuleList_RenderPlainEscapesServerText(t *testing.T) {
	r := ruleWithAttributes("r_1", corev1.Effect_EFFECT_BLOCK, "")
	r.GetRule().SetReason("IR-1\x1b[2J\tsecond column?")
	res := &ruleListResult{Rules: []ruleView{newRuleView(r, nil)}}

	plain := res.RenderPlain()
	assert.NotContains(t, plain, "\x1b")
	assert.Contains(t, plain, `IR-1\x1b[2J\tsecond column?`)
	lines := strings.Split(plain, "\n")
	require.Len(t, lines, 2)
	assert.Len(t, strings.Split(lines[1], "\t"), 10, "a tab in a value adds no column")
	assert.NotContains(t, res.RenderTable(), "\x1b")
}

func TestMapRestoreError_MissingRevision(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).Return(nil, status.Error(codes.NotFound, "no revision"))
	_, err := runChangeCreate(context.Background(), client, changeCreateInput{FromRevision: 99, PackageRules: "head"})
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "Policy revision 99 does not exist", ue.HumanError())
	assert.Equal(t, "List the revisions: safedep policy revision list", ue.Help())
}

func TestRunRuleList_AtRevisionResolvesTheOverrideAtThatRevision(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	// At revision 40 the override ov_pay had the name "payments-old". Head
	// renamed it, so a lookup at head would fail. The test sets no GetPolicy
	// expectation, so a read of head fails the test.
	old := headPolicy()
	old.GetOverrides()[0].SetName("payments-old")
	rev := &policyv1.GetPolicyRevisionResponse{}
	rev.SetRevision(revision(40, "old"))
	rev.SetPolicy(old)
	client.EXPECT().GetPolicyRevision(mock.Anything, mock.MatchedBy(func(r *policyv1.GetPolicyRevisionRequest) bool {
		return r.GetRevision() == 40
	})).Return(rev, nil).Once()

	res := &policyv1.ListPackageRulesResponse{}
	res.SetRules([]*policyv1.PackageRuleWithAttributes{ruleWithAttributes("r_77e", corev1.Effect_EFFECT_TRUST, "ov_pay")})
	client.EXPECT().ListPackageRules(mock.Anything, mock.MatchedBy(func(r *policyv1.ListPackageRulesRequest) bool {
		return r.GetFilters().GetOverrideId() == "ov_pay" && r.GetFilters().GetAtRevision() == 40
	})).Return(res, nil)

	got, err := runRuleList(context.Background(), client, &ruleListInput{AtRevision: 40, Override: "payments-old"})
	require.NoError(t, err)
	require.Len(t, got.Rules, 1)
	assert.Equal(t, `override "payments-old"`, got.Rules[0].AttachesTo)
}

func TestExpiryText(t *testing.T) {
	tests := []struct {
		name    string
		expires time.Time
		want    string
	}{
		{"within 30 days", fixedNow.Add(3 * 24 * time.Hour), "in 3d (2026-11-03)"},
		{"beyond 30 days", time.Date(2027, 1, 4, 0, 0, 0, 0, time.UTC), "2027-01-04"},
		{"in the past", fixedNow.Add(-2 * time.Hour), "2h ago (2026-10-31)"},
		{"seconds less than whole days", fixedNow.Add(7*24*time.Hour - 5*time.Second), "in 7d (2026-11-07)"},
		{"more than half a day over whole days", fixedNow.Add(3*24*time.Hour + 13*time.Hour), "in 4d (2026-11-04)"},
		{"days in the past", fixedNow.Add(-(2*24*time.Hour - time.Minute)), "2d ago (2026-10-29)"},
		{"less than a day", fixedNow.Add(20 * time.Hour), "in 20h (2026-11-01)"},
		{"days that round to 30", fixedNow.Add(30*24*time.Hour - time.Hour), "in 29d (2026-11-30)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, expiryText(tt.expires, fixedNow))
		})
	}
}

func TestRuleShow_ShowsTheExpiryDateOneTime(t *testing.T) {
	r := ruleWithAttributes("r_1", corev1.Effect_EFFECT_TRUST, "")
	r.GetRule().SetExpiresAt(timestamppb.New(fixedNow.Add(65 * 24 * time.Hour)))
	table := (&ruleShowResult{Rule: newRuleView(r, nil)}).table(fixedNow)
	assert.Equal(t, 1, strings.Count(table, "2027-01-04"), table)
}

func TestCreateRules_ReportsTheExpiryOfTheFlag(t *testing.T) {
	stderr := captureStderr(t)
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().CreatePackageRules(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req *policyv1.CreatePackageRulesRequest, _ ...grpc.CallOption) (*policyv1.CreatePackageRulesResponse, error) {
			r := ruleWithAttributes("r_1", corev1.Effect_EFFECT_TRUST, "")
			// The server keeps a time to the microsecond.
			r.GetRule().SetExpiresAt(timestamppb.New(req.GetRules()[0].GetRule().GetExpiresAt().AsTime().Truncate(time.Microsecond)))
			res := &policyv1.CreatePackageRulesResponse{}
			res.SetRules([]*policyv1.PackageRuleWithAttributes{r})
			res.SetRevision(revision(48, "trust"))
			return res, nil
		})
	// Each read of the clock is later than the one before it.
	now := fixedNow.Add(123456789 * time.Nanosecond)
	clock := func() time.Time {
		now = now.Add(5 * time.Millisecond)
		return now
	}
	in := ruleCreateInput{Trust: true, Selector: "pkg:npm/leftpad@1.0.0", Scopes: []string{"malware"}, Reason: "FP", Expires: "7d"}
	connect := func() (policyv1grpc.PolicyServiceClient, error) { return client, nil }

	require.NoError(t, createRules(context.Background(), connect, in, nil, clock))
	assert.Contains(t, stderr.String(), "The rule expires in 7d (2026-11-07)")
}

func TestAnnounceRuleCreate_WideBlock(t *testing.T) {
	tests := []struct {
		name     string
		override string
		want     string
	}{
		{"the policy base", "", "It stops every install of that ecosystem in the tenant"},
		{"a policy override", "payments", `It stops every install of that ecosystem on the subjects that policy override "payments" selects`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := captureStderr(t)
			in := ruleCreateInput{Block: true, Selector: "pkg:npm/*", Reason: "x", Override: tt.override}
			creates, err := buildRuleCreates(in, nil, fixedNow)
			require.NoError(t, err)
			announceRuleCreate(in, creates)
			assert.Contains(t, stderr.String(), tt.want)
			if tt.override != "" {
				assert.NotContains(t, stderr.String(), "in the tenant")
			}
		})
	}
}
