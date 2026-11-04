package policy

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/log"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
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

	// The longest values that the server accepts in the filters of
	// ListPolicyDecisionsRequest, in characters.
	maxRuleIDLen     = 64
	maxOverrideIDLen = 64
	maxPathLen       = 256
	maxProjectIDLen  = 128
	maxEndpointIDLen = 100
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

// decisionQuery is the checked input of one list. The filters hold every
// flag except --override, which needs the policy to resolve a name.
type decisionQuery struct {
	filters   *policyv1.ListPolicyDecisionsRequest_Filters
	override  string
	revision  uint64
	pageSize  uint32
	pageToken string
}

func decisionListCmd(a *app.App) *cobra.Command {
	var (
		in    decisionListInput
		query *decisionQuery
	)
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
			var err error
			query, err = newDecisionQuery(in, time.Now())
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runDecisionList(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), query)
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

// checkLength refuses locally a value that the server refuses for its length.
func checkLength(flag, value string, limit int) error {
	if n := utf8.RuneCountInString(value); n > limit {
		return invalidInputError(fmt.Errorf("--%s is %d characters long: the limit is %d", flag, n, limit),
			fmt.Sprintf("Give a --%s value of at most %d characters", flag, limit))
	}
	return nil
}

// newDecisionQuery checks the flags and builds the filters. It makes no
// RPC, so the command refuses a bad flag before it reads credentials.
func newDecisionQuery(in decisionListInput, now time.Time) (*decisionQuery, error) {
	filters := &policyv1.ListPolicyDecisionsRequest_Filters{}
	after, before, err := duration.Window(in.Since, in.Until, now, "decisions")
	if err != nil {
		return nil, err
	}
	if after != nil {
		filters.SetDecidedAfter(timestamppb.New(*after))
	}
	if before != nil {
		filters.SetDecidedBefore(timestamppb.New(*before))
	}
	if in.Tool != "" {
		n, err := parseDecisionEnum("tool", in.Tool, policyv1.DecisionTool_name, decisionToolPrefix, decisionToolTokens())
		if err != nil {
			return nil, err
		}
		filters.SetTool(policyv1.DecisionTool(n))
	}
	if in.Outcome != "" {
		n, err := parseDecisionEnum("outcome", in.Outcome, policyv1.DecisionOutcome_name, decisionOutcomePrefix,
			decisionOutcomeTokens())
		if err != nil {
			return nil, err
		}
		filters.SetOutcome(policyv1.DecisionOutcome(n))
	}
	if in.Revision > 0 {
		filters.SetRevision(in.Revision)
	}
	if in.Package != "" {
		pkg, err := parseDecisionPackage(in.Package)
		if err != nil {
			return nil, err
		}
		filters.SetPackage(pkg)
	}
	// An override name has at most 63 characters, so the limit of the id
	// also holds for a name.
	if err := checkLength("override", in.Override, maxOverrideIDLen); err != nil {
		return nil, err
	}
	for _, text := range []struct {
		flag  string
		value string
		limit int
		apply func(string)
	}{
		{"rule", in.Rule, maxRuleIDLen, filters.SetRuleId},
		{"path", in.Path, maxPathLen, filters.SetPath},
		{"project", in.Project, maxProjectIDLen, filters.SetProjectId},
		{"endpoint", in.Endpoint, maxEndpointIDLen, filters.SetEndpointId},
	} {
		if text.value == "" {
			continue
		}
		if err := checkLength(text.flag, text.value, text.limit); err != nil {
			return nil, err
		}
		text.apply(text.value)
	}
	return &decisionQuery{
		filters:   filters,
		override:  in.Override,
		revision:  in.Revision,
		pageSize:  in.PageSize,
		pageToken: in.PageToken,
	}, nil
}

// runDecisionList reads one page of decisions. It reads the policy only to
// resolve --override and to name the overrides that the decisions cite. It
// reads the policy at --revision when it is set, because the decisions of
// that revision used its overrides.
func runDecisionList(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	q *decisionQuery,
) (*decisionListResult, error) {
	var snapshot *policyv1.Policy
	if q.override != "" {
		_, p, err := readPolicyAt(ctx, client, q.revision)
		if err != nil {
			return nil, err
		}
		snapshot = p
		id, err := resolveOverrideID(p, q.override)
		if err != nil {
			return nil, err
		}
		q.filters.SetOverrideId(id)
	}
	req := &policyv1.ListPolicyDecisionsRequest{}
	req.SetPagination(paging.NewPaginationRequest(q.pageSize, q.pageToken))
	// Every filter is optional, so a set filter always adds bytes.
	if proto.Size(q.filters) > 0 {
		req.SetFilters(q.filters)
	}
	res, err := client.ListPolicyDecisions(ctx, req)
	if err != nil {
		return nil, mapDecisionListError(err)
	}

	names := map[string]string{}
	if citesOverride(res.GetDecisions()) {
		if snapshot == nil {
			_, p, err := readPolicyAt(ctx, client, q.revision)
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

// resolveOverrideID finds the override by name, then by id, in the policy
// that the command read. A stored decision keeps the id of an override that
// a later revision deleted, so a value with the shape of an override id goes
// to the server as is when the policy does not have it.
func resolveOverrideID(p *policyv1.Policy, ref string) (string, error) {
	o, err := findOverride(p, ref)
	if err == nil {
		return o.GetOverrideId(), nil
	}
	for _, o := range p.GetOverrides() {
		if o.GetOverrideId() == ref {
			return ref, nil
		}
	}
	if isOverrideID(ref) {
		return ref, nil
	}
	return "", err
}

const (
	overrideIDPrefix = "ov_"
	// overrideIDAlphabet is lowercase Crockford base32, the alphabet of the
	// ids that the server makes.
	overrideIDAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	minOverrideIDBody  = 8
)

// isOverrideID reports the shape of an id that the server gives an override:
// "ov_" and 8 to 64 characters of overrideIDAlphabet. It repeats id.Valid of
// dry, which the dry version of this CLI does not have yet.
func isOverrideID(s string) bool {
	body, ok := strings.CutPrefix(s, overrideIDPrefix)
	if !ok || len(body) < minOverrideIDBody || len(s) > maxOverrideIDLen {
		return false
	}
	for i := 0; i < len(body); i++ {
		if strings.IndexByte(overrideIDAlphabet, body[i]) < 0 {
			return false
		}
	}
	return true
}
