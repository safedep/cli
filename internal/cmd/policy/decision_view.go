package policy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/table"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

const (
	placeEndpoint    = "endpoint"
	placePullRequest = "pull-request"
	placeScan        = "scan"

	// shortSHA is the length of a commit hash in a table cell.
	shortSHA = 12
)

// decisionView is one decision as the table and the plain output show it.
// The JSON output does not use it: it writes the messages of the server.
type decisionView struct {
	ID         string
	DecidedAt  *time.Time
	Tool       string
	Outcome    string
	Ecosystem  string
	Name       string
	Version    string
	Revision   uint64
	DefaultRev uint64
	Layer      recordv1.Layer
	Path       string
	RuleID     string
	OverrideID string
	// override is the name of the override, or its id when the policy
	// that the command read does not have it.
	override  string
	PlaceKind string
	Place     string
	URL       string
}

type decisionListResult struct {
	decisions     []*policyv1.PolicyDecision
	views         []decisionView
	NextPageToken string
}

func newDecisionListResult(res *policyv1.ListPolicyDecisionsResponse, overrideNames map[string]string) *decisionListResult {
	out := &decisionListResult{
		decisions:     res.GetDecisions(),
		views:         make([]decisionView, 0, len(res.GetDecisions())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, d := range res.GetDecisions() {
		out.views = append(out.views, newDecisionView(d, overrideNames))
	}
	return out
}

func newDecisionView(d *policyv1.PolicyDecision, overrideNames map[string]string) decisionView {
	c := d.GetCitation()
	pv := d.GetPackageVersion()
	v := decisionView{
		ID:         d.GetDecisionId(),
		DecidedAt:  cloudpolicy.TimeOf(d.GetDecidedAt()),
		Tool:       tui.EnumToken(d.GetTool().String(), decisionToolPrefix),
		Outcome:    tui.EnumToken(d.GetOutcome().String(), decisionOutcomePrefix),
		Ecosystem:  tui.EnumToken(pv.GetPackage().GetEcosystem().String(), "ECOSYSTEM_"),
		Name:       pv.GetPackage().GetName(),
		Version:    pv.GetVersion(),
		Revision:   c.GetRevision(),
		DefaultRev: c.GetDefaultPolicyRevision(),
		Layer:      c.GetLayer(),
		Path:       c.GetPath(),
		RuleID:     c.GetRuleId(),
		OverrideID: c.GetOverrideId(),
		override:   c.GetOverrideId(),
	}
	if name := overrideNames[v.OverrideID]; name != "" {
		v.override = name
	}
	v.PlaceKind, v.Place, v.URL = describePlace(d.GetDecisionPoint())
	return v
}

// describePlace names the place of a decision in one short cell. For pmg it
// is the hostname and the user, and one hint of the context: the CI
// repository, else the Kubernetes namespace and workload, else the agent. In
// CI and in Kubernetes the hostname is the name of a runner or a Pod, so the
// repository or the workload is the useful name. One hint keeps the cell
// short. The JSON output has the full context.
func describePlace(p *messagescontroltowerv1.DecisionPoint) (kind, place, url string) {
	switch {
	case p.HasEndpoint():
		e := p.GetEndpoint()
		host := e.GetHostname()
		if host == "" {
			host = e.GetEndpointId()
		}
		if e.GetUsername() != "" {
			host += " (" + e.GetUsername() + ")"
		}
		parts := []string{host}
		k := e.GetKubernetes()
		switch {
		case e.GetCi().GetRepository() != "":
			parts = append(parts, "ci "+e.GetCi().GetRepository())
		case k.GetNamespace() != "" || k.GetWorkloadName() != "":
			parts = append(parts, "k8s "+strings.Trim(k.GetNamespace()+"/"+k.GetWorkloadName(), "/"))
		case e.GetAgent().GetAgentName() != "":
			parts = append(parts, "agent "+e.GetAgent().GetAgentName())
		}
		return placeEndpoint, strings.Join(parts, sep), ""
	case p.HasPullRequest():
		pr := p.GetPullRequest()
		return placePullRequest, fmt.Sprintf("%s#%d", pr.GetRepositoryName(), pr.GetNumber()), pr.GetUrl()
	case p.HasScan():
		s := p.GetScan()
		ref := s.GetRef()
		if ref == "" {
			ref = s.GetCommitSha()
			if len(ref) > shortSHA {
				ref = ref[:shortSHA]
			}
		}
		return placeScan, s.GetRepositoryName() + "@" + ref, s.GetUrl()
	}
	return "", "", ""
}

func (v decisionView) pkg() string {
	text := v.Ecosystem + "/" + v.Name
	if v.Version != "" {
		text += "@" + v.Version
	}
	return text
}

// decidedBy names the rule or the field that decided, and the layer or the
// override that set it.
func (v decisionView) decidedBy() string {
	var parts []string
	switch {
	case v.RuleID != "":
		parts = append(parts, "rule "+v.RuleID)
	case v.Path != "":
		parts = append(parts, v.Path)
	default:
		parts = append(parts, fixedRuleText(v.Tool))
	}
	switch {
	case v.override != "":
		parts = append(parts, "override "+v.override)
	case v.Layer == recordv1.Layer_LAYER_DEFAULT_POLICY:
		parts = append(parts, "default policy")
	case v.Layer == recordv1.Layer_LAYER_BASE:
		parts = append(parts, "base")
	}
	return cloudpolicy.EmptyDash(strings.Join(parts, sep))
}

// fixedRuleText names a decision whose citation has no rule and no path.
// The pull request check cites only the revisions for its verified malware
// guard and for its fixed license rule, because they are outside the policy
// contract. A stored decision does not tell the two apart.
func fixedRuleText(tool string) string {
	if tool == tui.EnumToken(policyv1.DecisionTool_DECISION_TOOL_PULL_REQUEST_CHECK.String(), decisionToolPrefix) {
		return "a fixed rule of the check"
	}
	return "a fixed rule of the tool"
}

// revisionCell gives the tenant revision, and the default policy revision
// when the default policy decided or the tenant has no revision.
func (v decisionView) revisionCell() string {
	var parts []string
	if v.Revision > 0 {
		parts = append(parts, fmt.Sprint(v.Revision))
	}
	if v.DefaultRev > 0 && (v.Layer == recordv1.Layer_LAYER_DEFAULT_POLICY || v.Revision == 0) {
		parts = append(parts, fmt.Sprintf("default %d", v.DefaultRev))
	}
	return cloudpolicy.EmptyDash(strings.Join(parts, sep))
}

func (v decisionView) layer() string {
	if v.Layer == recordv1.Layer_LAYER_UNSPECIFIED {
		return ""
	}
	return tui.EnumToken(v.Layer.String(), "LAYER_")
}

// RenderJSON writes the decisions as the server sent them, in protojson with
// the field names of the contract.
func (r *decisionListResult) RenderJSON() ([]byte, error) {
	decisions := make([]json.RawMessage, 0, len(r.decisions))
	for _, d := range r.decisions {
		b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(d)
		if err != nil {
			return nil, fmt.Errorf("policy decision list: encode decision %s: %w", d.GetDecisionId(), err)
		}
		decisions = append(decisions, b)
	}
	return json.MarshalIndent(struct {
		Decisions     []json.RawMessage `json:"decisions"`
		NextPageToken string            `json:"next_page_token,omitempty"`
	}{decisions, r.NextPageToken}, "", "  ")
}

func (r *decisionListResult) RenderTable() string {
	now := time.Now()
	withLink := false
	for _, v := range r.views {
		withLink = withLink || v.URL != ""
	}
	headers := []string{"WHEN", "TOOL", "PACKAGE", "OUTCOME", "DECIDED BY", "REV", "WHERE"}
	if withLink {
		headers = append(headers, "LINK")
	}
	rows := make([][]string, 0, len(r.views))
	for _, v := range r.views {
		row := []string{
			humanTimeCell(v.DecidedAt, now), v.Tool, v.pkg(), v.Outcome, v.decidedBy(), v.revisionCell(),
			cloudpolicy.EmptyDash(v.Place),
		}
		if withLink {
			row = append(row, cloudpolicy.EmptyDash(v.URL))
		}
		rows = append(rows, row)
	}
	t := table.New().
		Title("Policy decisions").
		Headers(headers...).
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No policy decisions match. Try a wider --since window or fewer filters.")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), cloudpolicy.Plural(len(rows), "decision", "decisions"))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *decisionListResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("decided_at\tdecision_id\ttool\toutcome\tecosystem\tpackage\tversion\trevision\t" +
		"default_policy_revision\tlayer\tpath\trule_id\toverride_id\tplace_kind\tplace\turl")
	for _, v := range r.views {
		b.WriteString("\n" + tui.PlainRow(cloudpolicy.RFC3339(v.DecidedAt), v.ID, v.Tool, v.Outcome, v.Ecosystem,
			v.Name, v.Version, fmt.Sprint(v.Revision), fmt.Sprint(v.DefaultRev), v.layer(), v.Path, v.RuleID,
			v.OverrideID, v.PlaceKind, v.Place, v.URL))
	}
	return b.String()
}
