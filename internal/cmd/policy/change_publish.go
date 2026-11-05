package policy

import (
	"context"
	"errors"
	"fmt"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/prompt"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

const directionLoosens = "LOOSENS"

// confirmFunc asks the user to confirm an action and returns the answer.
type confirmFunc func(question string) (bool, error)

// errPublishCancelled stops a publish that the user did not confirm.
var errPublishCancelled = errors.New("publish cancelled")

func changePublishCmd(a *app.App) *cobra.Command {
	var (
		message string
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "publish CHANGE",
		Short: "Publish an open policy change as a new policy revision",
		Long: "Publish an open policy change. The command previews the change first and shows its change " +
			"direction and every warning. A change that loosens the policy needs a confirmation: the " +
			"command asks in a terminal, and needs --yes when no terminal is present. The publish makes a " +
			"new policy revision for the whole tenant. Projects resolve it at their next check, and " +
			"endpoints apply it after their next policy fetch. If a part that the change edits moved " +
			"since its base revision, the publish stops. Then sync the change and review it again.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			rev, err := runChangePublish(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()),
				args[0], message, publishConfirm(yes, clitui.IsInteractive()))
			if errors.Is(err, errPublishCancelled) {
				tui.Info("Nothing is published")
				return nil
			}
			if err != nil {
				return err
			}
			reportPublish(rev.GetRevision(), directionToken(rev.GetDirection()))
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "the publish message, or the title of the change when empty")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "publish a change that loosens the policy with no prompt")
	return cmd
}

// publishConfirm asks in a terminal. With no terminal it needs --yes, so a
// script never loosens the policy by accident.
func publishConfirm(yes, interactive bool) confirmFunc {
	return func(question string) (bool, error) {
		if yes {
			return true, nil
		}
		if !interactive {
			cause := errors.New("the change loosens the policy, and no terminal is present to confirm it")
			return false, newPolicyError(usefulerror.ErrBadRequest, cause.Error(),
				"Review it with safedep policy change show, then pass --yes to publish it", cause)
		}
		return prompt.Confirm(question, false)
	}
}

func runChangePublish(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	id, message string,
	confirm confirmFunc,
) (*policyv1.PolicyRevision, error) {
	change, err := getChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	if err := requireOpen(change); err != nil {
		return nil, err
	}
	preview, err := previewChange(ctx, client, id)
	if err != nil {
		return nil, err
	}
	reportPreview(id, preview)
	if preview.Stale {
		cause := fmt.Errorf("policy change publish: change %s is stale at head revision %d", id, preview.HeadRevision)
		return nil, newPolicyError(usefulerror.ErrConflict,
			fmt.Sprintf("The policy moved: a part that change %s edits changed after its base revision", id),
			fmt.Sprintf("Sync it and review it again: safedep policy change sync %s", id),
			cause)
	}
	if preview.Direction == directionLoosens {
		ok, err := confirm(fmt.Sprintf("Change %s loosens the policy. Publish it?", clitui.EscapeLine(id)))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errPublishCancelled
		}
	}

	req := &policyv1.PublishPolicyChangeRequest{}
	req.SetChangeId(id)
	req.SetEtag(change.GetEtag())
	req.SetMessage(message)
	res, err := client.PublishPolicyChange(ctx, req)
	if err != nil {
		return nil, mapChangeError("publish", id, err)
	}
	return res.GetRevision(), nil
}

// reportPreview shows the change direction, each part that loosens the
// policy, and every warning of the preview, before the admin decides.
func reportPreview(id string, p *previewView) {
	tui.Info("Change %s%s%s", clitui.EscapeLine(id), sep, directionLine(p.Direction))
	reportLoosening(p)
	for _, w := range p.Warnings {
		subject := ""
		if w.Subject != "" {
			subject = " " + clitui.EscapeLine(w.Subject)
		}
		tui.Warning("%s%s: %s", clitui.EscapeLine(w.Code), subject, clitui.EscapeLine(w.Message))
	}
	if p.Stale {
		tui.Warning("Change %s is stale: the policy moved after its base revision. Sync it: safedep policy change sync %s",
			clitui.EscapeLine(id), clitui.EscapeLine(id))
	}
}

// reportLoosening names each part of a change that loosens the policy.
func reportLoosening(p *previewView) {
	for _, item := range p.Items {
		if item.Direction != directionLoosens {
			continue
		}
		if item.sentence != "" {
			tui.Warning("%s", clitui.EscapeLine(item.sentence))
			continue
		}
		tui.Warning("It loosens %s (%s -> %s) in %s", clitui.EscapeLine(item.What),
			clitui.EscapeLine(item.oldHuman), clitui.EscapeLine(item.newHuman), clitui.EscapeLine(item.Where))
	}
}

// reportPublish tells the admin the new revision, its change direction, when
// it applies, and how to find the endpoints that lag.
func reportPublish(revision uint64, direction string) {
	tui.Success("revision %d published%s%s", revision, sep, direction)
	tui.Info("  Projects resolve it at their next check. Endpoints apply it after their next policy fetch")
	tui.Info("  Endpoints that have not applied it yet: safedep endpoint list --applied-revision '<%d'", revision)
}
