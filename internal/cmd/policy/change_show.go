package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

const (
	overrideKindPrefix = "OVERRIDE_CHANGE_KIND_"
	warningCodePrefix  = "PREVIEW_WARNING_CODE_"
)

// changeItem is one row of "What changes": a field, a policy override or a
// package rule operation.
type changeItem struct {
	Where     string `json:"where"`
	What      string `json:"what"`
	Old       string `json:"old,omitempty"`
	New       string `json:"new,omitempty"`
	Direction string `json:"direction"`
	oldHuman  string
	newHuman  string
}

type coverageView struct {
	Path        string `json:"path"`
	Value       string `json:"value,omitempty"`
	Enforce     uint32 `json:"enforce"`
	Partial     uint32 `json:"partial"`
	Detect      uint32 `json:"detect"`
	Unsupported uint32 `json:"unsupported"`
}

type warningView struct {
	Code    string `json:"code"`
	Subject string `json:"subject,omitempty"`
	Message string `json:"message"`
}

type previewView struct {
	HeadRevision     uint64         `json:"head_revision"`
	Stale            bool           `json:"stale"`
	Direction        string         `json:"direction"`
	Items            []changeItem   `json:"changes"`
	MatchedProjects  uint32         `json:"matched_project_count"`
	MatchedEndpoints uint32         `json:"matched_endpoint_count"`
	Coverage         []coverageView `json:"coverage"`
	Warnings         []warningView  `json:"warnings"`
}

type changeShowResult struct {
	Change   changeView   `json:"change"`
	Restore  string       `json:"restore,omitempty"`
	Preview  *previewView `json:"preview,omitempty"`
	ruleOnly []changeItem
}

func changeShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show CHANGE",
		Short: "Show what a policy change does",
		Long: "Show a policy change. For an open change, the server previews it against head: the changed " +
			"fields and their change direction, the changes to policy overrides, the package rule " +
			"operations, the subjects that the change reaches, how well they enforce it, and warnings.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runChangeShow(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0])
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
}

func getChange(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) (*policyv1.PolicyChange, error) {
	req := &policyv1.GetPolicyChangeRequest{}
	req.SetChangeId(id)
	res, err := client.GetPolicyChange(ctx, req)
	if err != nil {
		return nil, mapChangeError("get", id, err)
	}
	return res.GetChange(), nil
}

func previewChange(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) (*previewView, error) {
	req := &policyv1.PreviewPolicyChangeRequest{}
	req.SetChangeId(id)
	res, err := client.PreviewPolicyChange(ctx, req)
	if err != nil {
		return nil, mapChangeError("preview", id, err)
	}
	return newPreviewView(res), nil
}

func runChangeShow(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) (*changeShowResult, error) {
	change, err := getChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	out := &changeShowResult{Change: newChangeView(change)}
	if change.HasRestore() {
		out.Restore = restoreSummary(change.GetRestore())
	}
	if change.GetState() != policyv1.PolicyChangeState_POLICY_CHANGE_STATE_OPEN {
		out.ruleOnly = ruleOperationItems(change.GetPackageRuleOperations())
		return out, nil
	}
	preview, err := previewChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	out.Preview = preview
	return out, nil
}

func restoreSummary(r *policyv1.RestoreSource) string {
	capabilities := "every capability"
	if len(r.GetCapabilities()) > 0 {
		capabilities = strings.Join(r.GetCapabilities(), ", ")
	}
	rules := "package rules stay at head"
	if r.GetPackageRules() == policyv1.PackageRuleRestore_PACKAGE_RULE_RESTORE_FROM_REVISION {
		rules = fmt.Sprintf("package rules from revision %d", r.GetFromRevision())
	}
	return fmt.Sprintf("restores %s from revision %d%s%s", capabilities, r.GetFromRevision(), sep, rules)
}

func newPreviewView(res *policyv1.PreviewPolicyChangeResponse) *previewView {
	v := &previewView{
		HeadRevision:     res.GetHeadRevision(),
		Stale:            res.GetStale(),
		Direction:        directionToken(res.GetDirection()),
		MatchedProjects:  res.GetMatchedProjectCount(),
		MatchedEndpoints: res.GetMatchedEndpointCount(),
		Items:            []changeItem{},
		Coverage:         []coverageView{},
		Warnings:         []warningView{},
	}
	for _, c := range res.GetChanges() {
		where := "base"
		if c.GetLayer() == recordv1.Layer_LAYER_OVERRIDE {
			where = fmt.Sprintf("override %q", c.GetOverrideName())
		}
		oldValue, oldHuman := cloudpolicy.FormatJSONValue(c.GetPath(), c.GetOldValue())
		newValue, newHuman := cloudpolicy.FormatJSONValue(c.GetPath(), c.GetNewValue())
		v.Items = append(v.Items, changeItem{
			Where: where, What: c.GetPath(), Old: oldValue, New: newValue,
			Direction: directionToken(c.GetDirection()), oldHuman: oldHuman, newHuman: newHuman,
		})
	}
	for _, c := range res.GetOverrideChanges() {
		v.Items = append(v.Items, changeItem{
			Where:     fmt.Sprintf("override %q", c.GetOverrideName()),
			What:      tui.EnumToken(c.GetKind().String(), overrideKindPrefix),
			Old:       c.GetOldValue(),
			New:       c.GetNewValue(),
			Direction: directionToken(c.GetDirection()),
			oldHuman:  emptyDash(c.GetOldValue()),
			newHuman:  emptyDash(c.GetNewValue()),
		})
	}
	for _, op := range res.GetPackageRuleOperations() {
		item := ruleOperationItem(op.GetOperation())
		item.Direction = directionToken(op.GetDirection())
		if op.GetOperation().HasCreate() {
			item.New = fmt.Sprintf("matches %d packages, seen on %d endpoints in 30d",
				op.GetMatchedPackageCount(), op.GetMatchedEndpointCount())
			item.newHuman = item.New
		}
		v.Items = append(v.Items, item)
	}
	for _, c := range res.GetCoverage() {
		value, _ := cloudpolicy.FormatJSONValue(c.GetPath(), c.GetValue())
		if c.GetValue() == "" {
			value = ""
		}
		v.Coverage = append(v.Coverage, coverageView{
			Path: c.GetPath(), Value: value, Enforce: c.GetEnforceCount(), Partial: c.GetPartialCount(),
			Detect: c.GetDetectCount(), Unsupported: c.GetUnsupportedCount(),
		})
	}
	for _, w := range res.GetWarnings() {
		v.Warnings = append(v.Warnings, warningView{
			Code:    strings.TrimPrefix(w.GetCode().String(), warningCodePrefix),
			Subject: w.GetSubject(),
			Message: w.GetMessage(),
		})
	}
	return v
}

func ruleOperationItems(ops []*policyv1.PackageRuleOperation) []changeItem {
	out := make([]changeItem, 0, len(ops))
	for _, op := range ops {
		out = append(out, ruleOperationItem(op))
	}
	return out
}

func ruleOperationItem(op *policyv1.PackageRuleOperation) changeItem {
	item := changeItem{Where: "package rules", oldHuman: "-", newHuman: "-"}
	switch {
	case op.HasCreate():
		rule := op.GetCreate().GetRule()
		item.What = fmt.Sprintf("create %s %s",
			tui.EnumToken(rule.GetEffect().String(), effectPrefix), selectorText(rule.GetSelector()))
		if id := op.GetCreate().GetOverrideId(); id != "" {
			item.Where = "package rules of override " + id
		}
	case op.HasUpdate():
		item.What = "update " + op.GetUpdate().GetRuleId()
	case op.HasDelete():
		item.What = "delete " + op.GetDelete().GetRuleId()
	}
	return item
}

func (r *changeShowResult) RenderJSON() ([]byte, error) {
	type out struct {
		*changeShowResult
		Operations []changeItem `json:"package_rule_operations,omitempty"`
	}
	return json.MarshalIndent(out{changeShowResult: r, Operations: r.ruleOnly}, "", "  ")
}

func (r *changeShowResult) headline() string {
	c := r.Change
	parts := []string{fmt.Sprintf("Change %s %q", c.ID, c.titleOrUntitled()), c.State, fmt.Sprintf("base rev %d", c.BaseRevision)}
	if c.PublishedRevision > 0 {
		parts = append(parts, fmt.Sprintf("published as rev %d", c.PublishedRevision))
	}
	if r.Preview != nil {
		parts = append(parts, fmt.Sprintf("head rev %d", r.Preview.HeadRevision))
		if r.Preview.Stale {
			parts = append(parts, "STALE: sync it before you publish")
		} else {
			parts = append(parts, "no conflict")
		}
	}
	return strings.Join(parts, sep)
}

func (r *changeShowResult) RenderTable() string {
	now := time.Now()
	sections := []string{r.headline()}
	details := [][2]string{{"Author", emptyDash(r.Change.Author)}, {"Updated", humanTimeCell(r.Change.updatedAt, now)}}
	if r.Restore != "" {
		details = append(details, [2]string{"Restore", r.Restore})
	}
	sections = append(sections, cloudpolicy.KeyValues(details))

	items := r.ruleOnly
	if r.Preview != nil {
		items = r.Preview.Items
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{it.Where, it.What, it.oldHuman + " -> " + it.newHuman, emptyDash(it.Direction)})
	}
	sections = append(sections, table.New().
		Title("What changes").
		Headers("WHERE", "WHAT", "OLD -> NEW", "DIRECTION").
		Rows(rows...).
		EmptyMessage("The change edits nothing.").
		Render())

	if p := r.Preview; p != nil {
		sections = append(sections, r.impactTable(p), r.warningTable(p),
			cloudpolicy.KeyValues([][2]string{{"Direction", directionLine(p.Direction)}}))
	}
	return strings.Join(sections, "\n\n")
}

func (r *changeShowResult) impactTable(p *previewView) string {
	rows := make([][]string, 0, len(p.Coverage))
	for _, c := range p.Coverage {
		what := c.Path
		if c.Value != "" {
			what += " = " + c.Value
		}
		rows = append(rows, []string{what, fmt.Sprint(c.Enforce), fmt.Sprint(c.Partial), fmt.Sprint(c.Detect), fmt.Sprint(c.Unsupported)})
	}
	return table.New().
		Title(fmt.Sprintf("Impact%ssubjects matched: %d %s, %d %s", sep,
			p.MatchedProjects, plural(int(p.MatchedProjects), "project", "projects"),
			p.MatchedEndpoints, plural(int(p.MatchedEndpoints), "endpoint", "endpoints"))).
		Headers("FIELD", "ENFORCED", "PARTIAL", "DETECT ONLY", "NOT SUPPORTED").
		Rows(rows...).
		EmptyMessage("No coverage data for the changed fields.").
		Render()
}

func (r *changeShowResult) warningTable(p *previewView) string {
	rows := make([][]string, 0, len(p.Warnings))
	for _, w := range p.Warnings {
		rows = append(rows, []string{w.Code, emptyDash(w.Subject), w.Message})
	}
	return table.New().
		Title("Warnings").
		Headers("CODE", "SUBJECT", "MESSAGE").
		Rows(rows...).
		EmptyMessage("No warnings.").
		Render()
}

func directionLine(direction string) string {
	switch direction {
	case "LOOSENS":
		return "LOOSENS. Only an Admin can publish it, and Admins and Owners get a notice"
	case "TIGHTENS":
		return "TIGHTENS. An Admin can publish it"
	default:
		return direction
	}
}

func (r *changeShowResult) RenderPlain() string {
	var b strings.Builder
	c := r.Change
	fmt.Fprintf(&b, "change_id\t%s\ntitle\t%s\nstate\t%s\nbase_revision\t%d", c.ID, c.Title, c.State, c.BaseRevision)
	items := r.ruleOnly
	if p := r.Preview; p != nil {
		fmt.Fprintf(&b, "\nhead_revision\t%d\nstale\t%t\ndirection\t%s", p.HeadRevision, p.Stale, p.Direction)
		items = p.Items
	}
	for _, it := range items {
		fmt.Fprintf(&b, "\nchange\t%s\t%s\t%s\t%s\t%s", it.Where, it.What, it.Old, it.New, it.Direction)
	}
	if p := r.Preview; p != nil {
		for _, w := range p.Warnings {
			fmt.Fprintf(&b, "\nwarning\t%s\t%s\t%s", w.Code, w.Subject, w.Message)
		}
	}
	return b.String()
}
