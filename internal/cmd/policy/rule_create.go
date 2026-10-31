package policy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/humanize"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
)

const (
	// maxBulkRules is the limit of rule operations in one change.
	maxBulkRules = 1000
	scopeAll     = "all"
)

type ruleCreateInput struct {
	Block       bool
	Trust       bool
	Selector    string
	File        string
	Vers        string
	Scopes      []string
	Reason      string
	Ticket      string
	RequestedBy string
	Expires     string
	Override    string
	Message     string
}

type ruleCreateResult struct {
	Rules    []*policyv1.PackageRuleWithAttributes
	Revision uint64
}

func ruleCreateCmd(a *app.App) *cobra.Command {
	var in ruleCreateInput
	cmd := &cobra.Command{
		Use:   "create (--block | --trust) [PURL]",
		Short: "Create a trust entry or block entries, and publish at once",
		Long: "Create a package rule and publish it at once as a new policy revision. A block entry " +
			"stops new installs of the packages that it selects. It can select a prefix that ends with " +
			"/* or .*, as in pkg:npm/@acme/*. Use --block -f FILE to block up to 1,000 selectors in one " +
			"revision: one PURL on each line, with an optional vers range after a space. A trust entry " +
			"selects one package and waives the verdicts of its --scope. It needs --expires. It never " +
			"waives verified malware. --scope all expands to every trust scope that this CLI knows. " +
			"A rule attaches to the policy base unless you pass --override NAME.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if len(args) == 1 {
				in.Selector = args[0]
			}
			return validateRuleCreate(in)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			rules, err := buildRuleCreates(in, cmd.InOrStdin(), time.Now())
			if err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			announceRuleCreate(in, rules)
			res, err := runRuleCreate(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), in, rules)
			if err != nil {
				return err
			}
			reportRuleCreate(in, res, time.Now())
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&in.Block, "block", false, "create block entries")
	f.BoolVar(&in.Trust, "trust", false, "create a trust entry")
	f.StringVarP(&in.File, "file", "f", "", "with --block: a file of selectors, one on each line; - reads stdin")
	f.StringVar(&in.Vers, "vers", "", "a version range, as in '>=1.2.0' or 'vers:npm/>=1.2.0|<2.0.0'")
	f.StringSliceVar(&in.Scopes, "scope", nil,
		"with --trust: the verdicts to waive ("+strings.Join(trustScopeTokens(), ", ")+", or all); repeatable")
	f.StringVar(&in.Reason, "reason", "", "why the rule exists (required); tools show it in a block message")
	f.StringVar(&in.Ticket, "ticket", "", "a ticket or incident reference, as in IR-2291")
	f.StringVar(&in.RequestedBy, "requested-by", "", "the team or person that asked for the rule")
	f.StringVar(&in.Expires, "expires", "", "when the rule stops: a duration, as in 30d, or a date (required with --trust)")
	f.StringVar(&in.Override, "override", "", "attach the rule to the policy override with this name; the policy base when empty")
	f.StringVarP(&in.Message, "message", "m", "", "the publish message; the server writes one when it is empty")
	return cmd
}

func validateRuleCreate(in ruleCreateInput) error {
	switch {
	case in.Block == in.Trust:
		return invalidInputError(errors.New("give one of --block or --trust"),
			"Block: safedep policy rule create --block pkg:npm/name --reason TEXT")
	case in.File != "" && in.Selector != "":
		return invalidInputError(errors.New("give a PURL or --file, not both"), "Use --file for a list of selectors")
	case in.File == "" && in.Selector == "":
		return invalidInputError(errors.New("give the PURL of the package"),
			"As in: safedep policy rule create --block pkg:npm/evil --reason TEXT")
	case in.File != "" && in.Trust:
		return invalidInputError(errors.New("only block entries can come from a file"),
			"Create each trust entry with its own command")
	case in.File != "" && in.Vers != "":
		return invalidInputError(errors.New("--vers applies to one PURL: in a file, write the range after the PURL"),
			"Write each line as: PURL [RANGE]")
	case strings.TrimSpace(in.Reason) == "":
		return invalidInputError(errors.New("--reason is required"), "Say why the rule exists, as in --reason \"IR-2291 credential stealer\"")
	case in.Trust && len(in.Scopes) == 0:
		return invalidInputError(errors.New("a trust entry needs --scope"),
			"Name each verdict that it waives: --scope "+strings.Join(trustScopeTokens(), ", --scope ")+", or --scope all")
	case in.Trust && in.Expires == "":
		return invalidInputError(errors.New("a trust entry needs --expires"), "As in --expires 30d")
	case in.Block && len(in.Scopes) > 0:
		return invalidInputError(errors.New("a block entry has no scope"), "Remove --scope")
	}
	_, err := expandScopes(in.Scopes)
	return err
}

// expandScopes reads the --scope values. "all" expands to every trust scope
// that this SDK knows, because the contract has no "all" value: a scope that
// a later release adds never widens a stored entry.
func expandScopes(values []string) ([]corev1.TrustScope, error) {
	var out []corev1.TrustScope
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), scopeAll) {
			out = out[:0]
			for _, token := range trustScopeTokens() {
				scope, err := parseTrustScope(token)
				if err != nil {
					return nil, err
				}
				out = append(out, scope)
			}
			return out, nil
		}
		scope, err := parseTrustScope(v)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	return out, nil
}

// buildRuleCreates checks every selector and builds the rules, before any
// RPC, so one bad line in a file stops the whole create.
func buildRuleCreates(in ruleCreateInput, stdin io.Reader, now time.Time) ([]*policyv1.PackageRuleCreate, error) {
	if err := validateRuleCreate(in); err != nil {
		return nil, err
	}
	effect := corev1.Effect_EFFECT_BLOCK
	if in.Trust {
		effect = corev1.Effect_EFFECT_TRUST
	}
	scopes, err := expandScopes(in.Scopes)
	if err != nil {
		return nil, err
	}
	var expires *timestamppb.Timestamp
	if in.Expires != "" {
		t, err := parseExpiry(in.Expires, now)
		if err != nil {
			return nil, invalidInputError(err, "Use a duration, as in 30d, or a date, as in 2027-01-26")
		}
		expires = timestamppb.New(t)
	}

	entries := []selectorEntry{{purl: in.Selector, vers: in.Vers}}
	if in.File != "" {
		entries, err = readSelectorFile(in.File, stdin)
		if err != nil {
			return nil, err
		}
	}

	creates := make([]*policyv1.PackageRuleCreate, 0, len(entries))
	for _, e := range entries {
		selector, err := buildSelector(e.purl, e.vers, effect)
		if err != nil {
			if e.line > 0 {
				err = fmt.Errorf("%s line %d: %w", in.File, e.line, err)
			}
			return nil, invalidInputError(err, "Fix the selector and run the command again")
		}
		rule := &corev1.PackageRule{}
		rule.SetSelector(selector)
		rule.SetEffect(effect)
		rule.SetScopes(scopes)
		rule.SetReason(in.Reason)
		if expires != nil {
			rule.SetExpiresAt(expires)
		}
		create := &policyv1.PackageRuleCreate{}
		create.SetRule(rule)
		create.SetTicket(in.Ticket)
		create.SetRequestedBy(in.RequestedBy)
		creates = append(creates, create)
	}
	return creates, nil
}

type selectorEntry struct {
	purl string
	vers string
	line int
}

// readSelectorFile reads one selector on each line: a PURL and an optional
// range after a space. A line that starts with # and an empty line are
// skipped.
func readSelectorFile(path string, stdin io.Reader) ([]selectorEntry, error) {
	r := stdin
	if path != "-" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, invalidInputError(fmt.Errorf("read %s: %w", path, err), "Check the path of --file")
		}
		r = bytes.NewReader(data)
	}
	var entries []selectorEntry
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 2 {
			return nil, invalidInputError(fmt.Errorf("%s line %d: write a PURL and an optional range, not %d values", path, n, len(fields)),
				"Write each line as: PURL [RANGE]")
		}
		entry := selectorEntry{purl: fields[0], line: n}
		if len(fields) == 2 {
			entry.vers = fields[1]
		}
		entries = append(entries, entry)
		if len(entries) > maxBulkRules {
			return nil, invalidInputError(fmt.Errorf("%s has more than %d selectors", path, maxBulkRules),
				fmt.Sprintf("Split the file into parts of %d lines or fewer", maxBulkRules))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, invalidInputError(fmt.Errorf("%s has no selectors", path), "Write one PURL on each line")
	}
	return entries, nil
}

func runRuleCreate(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	in ruleCreateInput,
	creates []*policyv1.PackageRuleCreate,
) (*ruleCreateResult, error) {
	if in.Override != "" {
		head, err := getPolicy(ctx, client)
		if err != nil {
			return nil, err
		}
		o, err := findOverride(head.GetPolicy(), in.Override)
		if err != nil {
			return nil, err
		}
		for _, c := range creates {
			c.SetOverrideId(o.GetOverrideId())
		}
	}
	req := &policyv1.CreatePackageRulesRequest{}
	req.SetRules(creates)
	req.SetMessage(in.Message)
	res, err := client.CreatePackageRules(ctx, req)
	if err != nil {
		return nil, mapRuleError("create", "", err)
	}
	return &ruleCreateResult{Rules: res.GetRules(), Revision: res.GetRevision().GetRevision()}, nil
}

// announceRuleCreate says what the rule does before the publish.
func announceRuleCreate(in ruleCreateInput, creates []*policyv1.PackageRuleCreate) {
	if in.Override == "" {
		tui.Info("Applies to every endpoint and project of the tenant (base). Use --override NAME to narrow it.")
	} else {
		tui.Info("Applies to the subjects that policy override %q selects.", in.Override)
	}
	if in.Block {
		tui.Info("A block stops new installs. It does not remove installed copies.")
		return
	}
	purl := creates[0].GetRule().GetSelector().GetPurl()
	for _, scope := range creates[0].GetRule().GetScopes() {
		tui.Warning("This rule loosens the policy: it waives %s for %s.", scopeEffect(scope), purl)
	}
	tui.Warning("Verified malware stays blocked")
}

func scopeEffect(scope corev1.TrustScope) string {
	switch scope {
	case corev1.TrustScope_TRUST_SCOPE_MALWARE:
		return "a malware verdict that is not verified"
	case corev1.TrustScope_TRUST_SCOPE_COOLDOWN:
		return "the dependency cooldown"
	case corev1.TrustScope_TRUST_SCOPE_VULNERABILITY:
		return "the vulnerability findings of a scan gate"
	default:
		return "the verdicts of scope " + scope.String()
	}
}

func reportRuleCreate(in ruleCreateInput, res *ruleCreateResult, now time.Time) {
	if len(res.Rules) == 1 {
		tui.Success("rule %s created%srevision %d published", res.Rules[0].GetRule().GetRuleId(), sep, res.Revision)
	} else {
		tui.Success("%d rules created%srevision %d published", len(res.Rules), sep, res.Revision)
	}
	if in.Trust && len(res.Rules) == 1 {
		if expires := timeOf(res.Rules[0].GetRule().GetExpiresAt()); expires != nil {
			tui.Info("  The rule expires %s (%s)", humanize.Time(*expires, now), expires.Format(time.DateOnly))
		}
	}
	tui.Info("  Each endpoint applies it after its next policy fetch. Pull request checks apply it at once.")
	tui.Info("  Endpoints that have not applied it yet: safedep endpoint list --applied-revision '<%d'", res.Revision)
}
