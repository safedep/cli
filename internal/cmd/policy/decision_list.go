package policy

import (
	"context"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	drypb "github.com/safedep/dry/api/pb"
	"github.com/safedep/dry/log"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const (
	decisionToolPrefix    = "DECISION_TOOL_"
	decisionOutcomePrefix = "DECISION_OUTCOME_"
)

type decisionListInput struct {
	Since     time.Duration
	Until     time.Duration
	Tool      string
	Outcome   string
	Revision  uint64
	Rule      string
	Override  string
	Path      string
	Package   string
	Project   string
	Endpoint  string
	PageSize  uint32
	PageToken string
}

func decisionListCmd(a *app.App) *cobra.Command {
	var in decisionListInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the decisions that the policy made, newest first",
		Long: "List the decisions that pmg and the pull request check made with the policy of the tenant, " +
			"newest first. Each row says which policy revision and which rule or field decided, and where: " +
			"the endpoint and the user for pmg, the repository and the pull request for a check. " +
			"--since and --until take a duration before now, as in 7d or 12h. --override takes the name " +
			"or the id of a policy override. --package takes a PURL with no version, as in pkg:npm/lodash. " +
			"The command reads one page. Pass --page-token to read the next page.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			_, err := newDecisionFilters(in, "", time.Now())
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runDecisionList(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), in, time.Now())
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.Var(duration.NewValue(&in.Since, 0), "since", "only decisions in this time before now, as in 7d or 12h")
	f.Var(duration.NewValue(&in.Until, 0), "until", "only decisions older than this time before now, as in 1d")
	f.StringVar(&in.Tool, "tool", "", "filter: the tool that decided ("+strings.Join(decisionToolTokens(), ", ")+")")
	f.StringVar(&in.Outcome, "outcome", "", "filter: what the decision did ("+strings.Join(decisionOutcomeTokens(), ", ")+")")
	f.Uint64Var(&in.Revision, "revision", 0, "filter: decisions that applied this policy revision")
	f.StringVar(&in.Rule, "rule", "", "filter: decisions that this package rule made, by rule id")
	f.StringVar(&in.Override, "override", "", "filter: decisions that this policy override made, by name or id")
	f.StringVar(&in.Path, "path", "", "filter: decisions that this field made, as in package_admission.cooldown.min_age")
	f.StringVar(&in.Package, "package", "", "filter: decisions about this package in all its versions, as in pkg:npm/lodash")
	f.StringVar(&in.Project, "project", "", "filter: decisions at a repository that is bound to this project id")
	f.StringVar(&in.Endpoint, "endpoint", "", "filter: decisions that pmg made on this endpoint id")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func decisionToolTokens() []string {
	return tui.EnumTokens(policyv1.DecisionTool_name, decisionToolPrefix)
}

func decisionOutcomeTokens() []string {
	return tui.EnumTokens(policyv1.DecisionOutcome_name, decisionOutcomePrefix)
}

func parseDecisionEnum(flag, token string, names map[int32]string, prefix string, tokens []string) (int32, error) {
	n, ok := tui.ParseEnumToken(names, prefix, token)
	if !ok {
		return 0, invalidInputError(
			fmt.Errorf("unknown --%s value %q: allowed values are %s", flag, token, strings.Join(tokens, ", ")),
			"Retry with one of: "+strings.Join(tokens, ", "))
	}
	return n, nil
}

// parseDecisionPackage reads a PURL with no version. The filter matches every
// version of the package, so a version in the PURL would mislead.
func parseDecisionPackage(purl string) (*packagev1.Package, error) {
	help := "Give a PURL with no version, as in --package pkg:npm/lodash"
	if !strings.HasPrefix(purl, "pkg:") {
		return nil, invalidInputError(fmt.Errorf("--package %q is not a PURL", purl), help)
	}
	parsed, err := drypb.NewPurlPackageVersion(purl)
	if err != nil {
		return nil, invalidInputError(fmt.Errorf("--package %q: %w", purl, err), help)
	}
	pv := parsed.PackageVersion()
	switch {
	case pv.GetPackage().GetEcosystem() == packagev1.Ecosystem_ECOSYSTEM_UNSPECIFIED:
		return nil, invalidInputError(fmt.Errorf("--package %q: unknown ecosystem", purl), help)
	case pv.GetVersion() != "":
		return nil, invalidInputError(
			fmt.Errorf("--package %q has a version: the filter matches every version of the package", purl), help)
	case pv.GetPackage().GetName() == "":
		return nil, invalidInputError(fmt.Errorf("--package %q has no name", purl), help)
	}
	return pv.GetPackage(), nil
}

// newDecisionFilters builds the request filters from the flags. overrideID is
// the id that --override resolved to. It returns nil when no filter is set.
func newDecisionFilters(
	in decisionListInput,
	overrideID string,
	now time.Time,
) (*policyv1.ListPolicyDecisionsRequest_Filters, error) {
	filters := &policyv1.ListPolicyDecisionsRequest_Filters{}
	set := false
	after, before, err := duration.Window(in.Since, in.Until, now)
	if err != nil {
		return nil, err
	}
	if after != nil {
		filters.SetDecidedAfter(timestamppb.New(*after))
		set = true
	}
	if before != nil {
		filters.SetDecidedBefore(timestamppb.New(*before))
		set = true
	}
	if in.Tool != "" {
		n, err := parseDecisionEnum("tool", in.Tool, policyv1.DecisionTool_name, decisionToolPrefix, decisionToolTokens())
		if err != nil {
			return nil, err
		}
		filters.SetTool(policyv1.DecisionTool(n))
		set = true
	}
	if in.Outcome != "" {
		n, err := parseDecisionEnum("outcome", in.Outcome, policyv1.DecisionOutcome_name, decisionOutcomePrefix,
			decisionOutcomeTokens())
		if err != nil {
			return nil, err
		}
		filters.SetOutcome(policyv1.DecisionOutcome(n))
		set = true
	}
	if in.Revision > 0 {
		filters.SetRevision(in.Revision)
		set = true
	}
	if in.Package != "" {
		pkg, err := parseDecisionPackage(in.Package)
		if err != nil {
			return nil, err
		}
		filters.SetPackage(pkg)
		set = true
	}
	for _, text := range []struct {
		value string
		apply func(string)
	}{
		{in.Rule, filters.SetRuleId},
		{overrideID, filters.SetOverrideId},
		{in.Path, filters.SetPath},
		{in.Project, filters.SetProjectId},
		{in.Endpoint, filters.SetEndpointId},
	} {
		if text.value != "" {
			text.apply(text.value)
			set = true
		}
	}
	if !set {
		return nil, nil
	}
	return filters, nil
}

// runDecisionList reads one page of decisions. It reads the policy only to
// resolve --override and to name the overrides that the decisions cite. It
// reads the policy at --revision when it is set, because the decisions of
// that revision used its overrides.
func runDecisionList(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	in decisionListInput,
	now time.Time,
) (*decisionListResult, error) {
	if _, err := newDecisionFilters(in, "", now); err != nil {
		return nil, err
	}
	var snapshot *policyv1.Policy
	overrideID := ""
	if in.Override != "" {
		_, p, err := readPolicyAt(ctx, client, in.Revision)
		if err != nil {
			return nil, err
		}
		snapshot = p
		o, err := findOverrideByNameOrID(p, in.Override)
		if err != nil {
			return nil, err
		}
		overrideID = o.GetOverrideId()
	}
	filters, err := newDecisionFilters(in, overrideID, now)
	if err != nil {
		return nil, err
	}
	req := &policyv1.ListPolicyDecisionsRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if filters != nil {
		req.SetFilters(filters)
	}
	res, err := client.ListPolicyDecisions(ctx, req)
	if err != nil {
		return nil, mapDecisionListError(err)
	}

	names := map[string]string{}
	if citesOverride(res.GetDecisions()) {
		if snapshot == nil {
			_, p, err := readPolicyAt(ctx, client, in.Revision)
			if err != nil {
				// The names only label the output. The ids stay, so the list is
				// still correct without them.
				log.Warnf("policy decision list: read the override names: %v", err)
			}
			snapshot = p
		}
		names = overrideNames(snapshot)
	}
	return newDecisionListResult(res, names), nil
}

func citesOverride(decisions []*policyv1.PolicyDecision) bool {
	for _, d := range decisions {
		if d.GetCitation().GetOverrideId() != "" {
			return true
		}
	}
	return false
}

// findOverrideByNameOrID accepts the id too, because the JSON output of a
// decision gives the id of its override. A name wins over an id.
func findOverrideByNameOrID(p *policyv1.Policy, ref string) (*policyv1.Override, error) {
	o, err := findOverride(p, ref)
	if err == nil {
		return o, nil
	}
	for _, o := range p.GetOverrides() {
		if o.GetOverrideId() == ref {
			return o, nil
		}
	}
	return nil, err
}
