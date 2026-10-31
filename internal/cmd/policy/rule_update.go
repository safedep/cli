package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

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
			res, err := runRuleUpdate(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), update, in.Message, time.Now())
			if err != nil {
				return err
			}
			id := clitui.EscapeLine(args[0])
			msg := fmt.Sprintf("rule %s updated", id)
			if update.HasExpiresAt() {
				msg = fmt.Sprintf("rule %s now expires on %s", id, update.GetExpiresAt().AsTime().Format(time.DateOnly))
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
	now time.Time,
) (*policyv1.UpdatePackageRuleResponse, error) {
	if update.HasExpiresAt() && update.GetExpiresAt().AsTime().Sub(now) > maxTrustExpiry {
		if err := checkUpdatedTrustExpiry(ctx, client, update, now); err != nil {
			return nil, err
		}
	}
	req := &policyv1.UpdatePackageRuleRequest{}
	req.SetUpdate(update)
	req.SetMessage(message)
	res, err := client.UpdatePackageRule(ctx, req)
	if err != nil {
		return nil, mapRuleError("update", update.GetRuleId(), err)
	}
	return res, nil
}

// checkUpdatedTrustExpiry reads the rule, because only a trust entry has the
// 90-day limit. A block entry can expire later.
func checkUpdatedTrustExpiry(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	update *policyv1.PackageRuleUpdate,
	now time.Time,
) error {
	req := &policyv1.GetPackageRuleRequest{}
	req.SetRuleId(update.GetRuleId())
	res, err := client.GetPackageRule(ctx, req)
	if err != nil {
		return mapRuleError("update", update.GetRuleId(), err)
	}
	if res.GetRule().GetRule().GetEffect() != corev1.Effect_EFFECT_TRUST {
		return nil
	}
	return checkTrustExpiry(update.GetExpiresAt().AsTime(), now)
}
