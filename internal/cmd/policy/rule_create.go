package policy

import (
	"context"
	"io"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

const (
	// maxBulkRules is the limit of rule operations in one change.
	maxBulkRules = 1000
	scopeAll     = "all"
)

type ruleCreateInput struct {
	Block           bool
	Trust           bool
	Selector        string
	File            string
	Vers            string
	Scopes          []string
	Vulnerabilities []string
	Reason          string
	Ticket          string
	RequestedBy     string
	Expires         string
	Override        string
	Message         string
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
			"--scope vulnerability with --vulnerability ID[:JUSTIFICATION] waives only the named " +
			"vulnerabilities, and every other vulnerability of the package still fails a scan gate. " +
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
			connect := func() (policyv1grpc.PolicyServiceClient, error) {
				client, err := a.ControlPlane()
				if err != nil {
					return nil, err
				}
				return cloudpolicy.NewPolicyClient(client.Connection()), nil
			}
			return createRules(cmd.Context(), connect, in, cmd.InOrStdin(), time.Now)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&in.Block, "block", false, "create block entries")
	f.BoolVar(&in.Trust, "trust", false, "create a trust entry")
	f.StringVarP(&in.File, "file", "f", "", "with --block: a file of selectors, one on each line, or - for stdin")
	f.StringVar(&in.Vers, "vers", "", "a version range, as in '>=1.2.0' or 'vers:npm/>=1.2.0|<2.0.0'")
	f.StringSliceVar(&in.Scopes, "scope", nil,
		"with --trust: a verdict to waive, one of "+strings.Join(createScopeTokens(), ", ")+", or all (repeatable)")
	f.StringArrayVar(&in.Vulnerabilities, "vulnerability", nil,
		"with --scope vulnerability: waive only this id, as CVE-2024-3094, with an optional :JUSTIFICATION, one of "+
			strings.Join(justificationNames(), ", ")+" (repeatable)")
	f.StringVar(&in.Reason, "reason", "", "why the rule exists, which tools show in a block message (required)")
	f.StringVar(&in.Ticket, "ticket", "", "a ticket or incident reference, as in IR-2291")
	f.StringVar(&in.RequestedBy, "requested-by", "", "the team or person that asked for the rule")
	f.StringVar(&in.Expires, "expires", "", "when the rule stops: a duration, as in 30d, or a date (required with --trust)")
	f.StringVar(&in.Override, "override", "", "the policy override that the rule attaches to, or the policy base when empty")
	f.StringVarP(&in.Message, "message", "m", "", "the publish message, or a message that the server writes when empty")
	return cmd
}

// createRules reads the clock one time. The expiry of the rule and the
// report of it use the same instant, so --expires 7d reports "in 7d" and not
// "in 6d". The instant has no fraction of a second, because the server keeps
// a time to the microsecond and a rounded expiry would show one day less.
func createRules(
	ctx context.Context,
	connect func() (policyv1grpc.PolicyServiceClient, error),
	in ruleCreateInput,
	stdin io.Reader,
	clock func() time.Time,
) error {
	now := clock().Truncate(time.Second)
	rules, err := buildRuleCreates(in, stdin, now)
	if err != nil {
		return err
	}
	client, err := connect()
	if err != nil {
		return err
	}
	announceRuleCreate(in, rules)
	res, err := runRuleCreate(ctx, client, in, rules)
	if err != nil {
		return err
	}
	reportRuleCreate(in, res, now)
	return nil
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
		tui.Info("Applies to the subjects that policy override %q selects.", clitui.EscapeLine(in.Override))
	}
	if in.Block {
		for _, c := range creates {
			if purl := c.GetRule().GetSelector().GetPurl(); isWideSelector(purl) {
				tui.Warning("Selector %s blocks every package of its ecosystem. It stops every install of that ecosystem %s",
					clitui.EscapeLine(purl), blockPlace(in.Override))
			}
		}
		tui.Info("A block stops new installs. It does not remove installed copies.")
		return
	}
	rule := creates[0].GetRule()
	purl := rule.GetSelector().GetPurl()
	for _, scope := range rule.GetScopes() {
		tui.Warning("This rule loosens the policy: it waives %s for %s.", scopeEffect(rule, scope), clitui.EscapeLine(purl))
	}
	if len(rule.GetVulnerabilities()) > 0 {
		tui.Info("A scan gate still fails on every other vulnerability of the package.")
	}
	tui.Warning("Verified malware stays blocked")
}

func reportRuleCreate(in ruleCreateInput, res *ruleCreateResult, now time.Time) {
	if len(res.Rules) == 1 {
		tui.Success("rule %s created%srevision %d published", clitui.EscapeLine(res.Rules[0].GetRule().GetRuleId()), sep, res.Revision)
	} else {
		tui.Success("%d rules created%srevision %d published", len(res.Rules), sep, res.Revision)
	}
	if in.Trust && len(res.Rules) == 1 {
		if expires := cloudpolicy.TimeOf(res.Rules[0].GetRule().GetExpiresAt()); expires != nil {
			tui.Info("  The rule expires %s", expiryText(*expires, now))
		}
	}
	tui.Info("  Each endpoint applies it after its next policy fetch. Pull request checks apply it at once.")
	reportLaggingEndpoints(res.Revision)
}
