package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
)

type ruleShowResult struct {
	Rule ruleView `json:"rule"`
}

func ruleShowCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show RULE",
		Short: "Show one package rule",
		Long: "Show one package rule: its selector, effect, trust scopes, reason, ticket, requester, the " +
			"policy override that it attaches to, its expiry, and the revisions that added and removed it. " +
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
	v := r.Rule
	now := time.Now()
	removed := "active"
	if v.RemovedRevision > 0 {
		removed = fmt.Sprintf("removed in revision %d", v.RemovedRevision)
	}
	expires := v.expiresCell(now)
	if v.expires != nil {
		expires += " (" + v.expires.Format(time.DateOnly) + ")"
	}
	return cloudpolicy.KeyValues([][2]string{
		{"Rule", v.ID},
		{"Selector", v.selector()},
		{"Effect", v.Effect},
		{"Scope", emptyDash(strings.Join(v.Scopes, ", "))},
		{"Reason", v.Reason},
		{"Ticket", emptyDash(v.Ticket)},
		{"Requested by", emptyDash(v.RequestedBy)},
		{"Attaches to", v.AttachesTo},
		{"Expires", expires},
		{"Created by", emptyDash(v.CreatedBy)},
		{"Revisions", fmt.Sprintf("added in revision %d%s%s", v.AddedRevision, sep, removed)},
	})
}

func (r *ruleShowResult) RenderPlain() string {
	v := r.Rule
	return strings.Join([]string{
		"rule_id\t" + v.ID,
		"purl\t" + v.Purl,
		"vers\t" + v.Vers,
		"effect\t" + v.Effect,
		"scopes\t" + strings.Join(v.Scopes, ","),
		"reason\t" + v.Reason,
		"ticket\t" + v.Ticket,
		"requested_by\t" + v.RequestedBy,
		"override_id\t" + v.OverrideID,
		"expires_at\t" + v.ExpiresAt,
		"created_by\t" + v.CreatedBy,
		fmt.Sprintf("added_revision\t%d", v.AddedRevision),
		fmt.Sprintf("removed_revision\t%d", v.RemovedRevision),
	}, "\n")
}

type ruleUpdateInput struct {
	Reason      string
	Expires     string
	Ticket      string
	RequestedBy string
	Message     string
	changed     func(name string) bool
}

func ruleUpdateCmd(a *app.App) *cobra.Command {
	var in ruleUpdateInput
	cmd := &cobra.Command{
		Use:   "update RULE",
		Short: "Change the reason, expiry, ticket or requester of a package rule",
		Long: "Change a package rule and publish the change at once as a new policy revision. A rule never " +
			"changes its selector, effect or trust scopes: delete it and create another. A later expiry " +
			"of a trust entry loosens the policy.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in.changed = cmd.Flags().Changed
			update, err := buildRuleUpdate(args[0], in, time.Now())
			if err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runRuleUpdate(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), update, in.Message)
			if err != nil {
				return err
			}
			msg := fmt.Sprintf("rule %s updated", args[0])
			if update.HasExpiresAt() {
				msg = fmt.Sprintf("rule %s now expires on %s", args[0], update.GetExpiresAt().AsTime().Format(time.DateOnly))
			}
			tui.Success("%s%srevision %d published", msg, sep, res.GetRevision().GetRevision())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Reason, "reason", "", "the new reason")
	f.StringVar(&in.Expires, "expires", "", "the new expiry: a duration from now, as in 90d, or a date")
	f.StringVar(&in.Ticket, "ticket", "", "the new ticket or incident reference")
	f.StringVar(&in.RequestedBy, "requested-by", "", "the new team or person that asked for the rule")
	f.StringVarP(&in.Message, "message", "m", "", "the publish message; the server writes one when it is empty")
	return cmd
}

func buildRuleUpdate(id string, in ruleUpdateInput, now time.Time) (*policyv1.PackageRuleUpdate, error) {
	changed := in.changed
	if changed == nil {
		changed = func(string) bool { return false }
	}
	update := &policyv1.PackageRuleUpdate{}
	update.SetRuleId(id)
	set := false
	if changed("reason") {
		if strings.TrimSpace(in.Reason) == "" {
			return nil, invalidInputError(errors.New("--reason must not be empty"), "Say why the rule exists")
		}
		update.SetReason(in.Reason)
		set = true
	}
	if changed("expires") {
		t, err := parseExpiry(in.Expires, now)
		if err != nil {
			return nil, invalidInputError(err, "Use a duration, as in 90d, or a date, as in 2027-01-26")
		}
		update.SetExpiresAt(timestamppb.New(t))
		set = true
	}
	if changed("ticket") {
		update.SetTicket(in.Ticket)
		set = true
	}
	if changed("requested-by") {
		update.SetRequestedBy(in.RequestedBy)
		set = true
	}
	if !set {
		return nil, invalidInputError(errors.New("give at least one of --reason, --expires, --ticket or --requested-by"),
			"As in: safedep policy rule update "+id+" --expires 90d")
	}
	return update, nil
}

func runRuleUpdate(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	update *policyv1.PackageRuleUpdate,
	message string,
) (*policyv1.UpdatePackageRuleResponse, error) {
	req := &policyv1.UpdatePackageRuleRequest{}
	req.SetUpdate(update)
	req.SetMessage(message)
	res, err := client.UpdatePackageRule(ctx, req)
	if err != nil {
		return nil, mapRuleError("update", update.GetRuleId(), err)
	}
	return res, nil
}

func ruleDeleteCmd(a *app.App) *cobra.Command {
	var reason, message string
	cmd := &cobra.Command{
		Use:   "delete RULE --reason TEXT",
		Short: "Delete a package rule and publish at once",
		Long: "Delete a package rule and publish the change at once as a new policy revision. The rule " +
			"stays in the history of the revisions. The delete of a block entry loosens the policy.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return invalidInputError(errors.New("--reason is required"), "Say why the rule goes, as in --reason \"no longer needed\"")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			rev, err := runRuleDelete(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), args[0], reason, message)
			if err != nil {
				return err
			}
			tui.Success("rule %s deleted%srevision %d published", args[0], sep, rev)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the rule goes (required)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "the publish message; the server writes one when it is empty")
	return cmd
}

func runRuleDelete(ctx context.Context, client policyv1grpc.PolicyServiceClient, id, reason, message string) (uint64, error) {
	del := &policyv1.PackageRuleDelete{}
	del.SetRuleId(id)
	del.SetReason(reason)
	req := &policyv1.DeletePackageRuleRequest{}
	req.SetDelete(del)
	req.SetMessage(message)
	res, err := client.DeletePackageRule(ctx, req)
	if err != nil {
		return 0, mapRuleError("delete", id, err)
	}
	return res.GetRevision().GetRevision(), nil
}
