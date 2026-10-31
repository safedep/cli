package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
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

func (r *changeShowResult) RenderJSON() ([]byte, error) {
	type out struct {
		*changeShowResult
		Operations []changeItem `json:"package_rule_operations,omitempty"`
	}
	return json.MarshalIndent(out{changeShowResult: r, Operations: r.ruleOnly}, "", "  ")
}

func (r *changeShowResult) headline() string {
	c := r.Change
	parts := []string{
		fmt.Sprintf("Change %s \"%s\"", tui.EscapeLine(c.ID), tui.EscapeLine(c.titleOrUntitled())),
		c.State, fmt.Sprintf("base rev %d", c.BaseRevision),
	}
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
	details := [][2]string{{"Author", cloudpolicy.EmptyDash(r.Change.Author)}, {"Updated", humanTimeCell(r.Change.updatedAt, now)}}
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
		rows = append(rows, []string{it.Where, it.What, it.oldHuman + " -> " + it.newHuman, cloudpolicy.EmptyDash(it.Direction)})
	}
	sections = append(sections, table.New().
		Title("What changes").
		Headers("WHERE", "WHAT", "OLD -> NEW", "DIRECTION").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("The change edits nothing.").
		Render())

	if p := r.Preview; p != nil {
		sections = append(sections, r.impactTable(p), r.warningTable(p),
			cloudpolicy.KeyValues([][2]string{{"Direction", directionLine(p.Direction)}}))
	}
	return strings.Join(sections, "\n\n")
}
