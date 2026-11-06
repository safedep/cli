package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

type ruleShowResult struct {
	Rule ruleView `json:"rule"`
}

func ruleShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show RULE",
		Short: "Show one package rule",
		Long: "Show one package rule: its selector, effect, trust scopes, reason, ticket, requester, the " +
			"policy override that it attaches to, its expiry, and the revisions that added, updated and removed it. " +
			"A block message of a tool cites the rule id.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runRuleShow(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0])
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
}

func runRuleShow(ctx context.Context, client policyv1grpc.PolicyServiceClient, id string) (*ruleShowResult, error) {
	req := &policyv1.GetPackageRuleRequest{}
	req.SetRuleId(id)
	res, err := client.GetPackageRule(ctx, req)
	if err != nil {
		return nil, mapRuleError("show", id, err)
	}
	var names map[string]string
	if res.GetRule().GetOverrideId() != "" {
		head, err := getPolicy(ctx, client)
		if err != nil {
			return nil, err
		}
		names = overrideNames(head.GetPolicy())
	}
	return &ruleShowResult{Rule: newRuleView(res.GetRule(), names)}, nil
}

func (r *ruleShowResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *ruleShowResult) RenderTable() string {
	return r.table(time.Now())
}

func (r *ruleShowResult) table(now time.Time) string {
	v := r.Rule
	removed := "active"
	if v.RemovedRevision > 0 {
		removed = fmt.Sprintf("removed in revision %d", v.RemovedRevision)
	}
	expires := "never"
	if v.expires != nil {
		expires = expiryText(*v.expires, now)
	}
	return cloudpolicy.KeyValues([][2]string{
		{"Rule", v.ID},
		{"Selector", v.selector()},
		{"Effect", v.Effect},
		{"Scope", cloudpolicy.EmptyDash(strings.Join(v.Scopes, ", "))},
		{"Reason", v.Reason},
		{"Ticket", cloudpolicy.EmptyDash(v.Ticket)},
		{"Requested by", cloudpolicy.EmptyDash(v.RequestedBy)},
		{"Attaches to", v.AttachesTo},
		{"Expires", expires},
		{"Created by", cloudpolicy.EmptyDash(v.CreatedBy)},
		{"Revisions", v.revisions() + sep + removed},
	})
}

// revisions names the revision that added the rule, and the revision of its
// last update when an update changed it later.
func (v ruleView) revisions() string {
	text := fmt.Sprintf("added in revision %d", v.AddedRevision)
	if v.UpdatedRevision > 0 && v.UpdatedRevision != v.AddedRevision {
		text += fmt.Sprintf(", updated in revision %d", v.UpdatedRevision)
	}
	return text
}

func (r *ruleShowResult) RenderPlain() string {
	v := r.Rule
	return strings.Join([]string{
		clitui.PlainRow("rule_id", v.ID),
		clitui.PlainRow("purl", v.Purl),
		clitui.PlainRow("vers", v.Vers),
		clitui.PlainRow("effect", v.Effect),
		clitui.PlainRow("scopes", strings.Join(v.Scopes, ",")),
		clitui.PlainRow("reason", v.Reason),
		clitui.PlainRow("ticket", v.Ticket),
		clitui.PlainRow("requested_by", v.RequestedBy),
		clitui.PlainRow("override_id", v.OverrideID),
		clitui.PlainRow("expires_at", v.ExpiresAt),
		clitui.PlainRow("created_by", v.CreatedBy),
		clitui.PlainRow("added_revision", fmt.Sprint(v.AddedRevision)),
		clitui.PlainRow("updated_revision", fmt.Sprint(v.UpdatedRevision)),
		clitui.PlainRow("removed_revision", fmt.Sprint(v.RemovedRevision)),
	}, "\n")
}
