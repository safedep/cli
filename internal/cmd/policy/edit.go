package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/config"
	clitui "github.com/safedep/cli/internal/tui"
)

// settingsReferenceURL is the page that describes each policy settings field
// and its values.
const settingsReferenceURL = "https://docs.safedep.io/reference/cloud-policy-settings"

type editInput struct {
	Title    string
	Override string
}

func editCmd(a *app.App) *cobra.Command {
	var in editInput
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit the policy settings document in your editor",
		Long: "Open the policy settings document of the tenant in $VISUAL or $EDITOR: the policy base and " +
			"every policy override. Saving creates a policy change on the server. Nothing applies until " +
			"you publish the change. Use --override NAME to edit one policy override. Package rules are " +
			"not in the document. Change them with `safedep policy rule`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireTerminal(); err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			change, err := runEdit(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()),
				openEditor, a.Tenant(), in)
			if errors.Is(err, errNoChange) {
				tui.Info("No change. Nothing is saved")
				return nil
			}
			if err != nil {
				return err
			}
			id := clitui.EscapeLine(change.GetChangeId())
			tui.Success("change %s saved from revision %d", id, change.GetBaseRevision())
			tui.Info("  Review it: safedep policy change show %s", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&in.Title, "message", "m", "", "the title of the policy change")
	cmd.Flags().StringVar(&in.Override, "override", "", "edit only the policy override with this name")
	return cmd
}

func openEditor(ctx context.Context, path string) error {
	return clitui.RunEditor(ctx, config.Editor(), path)
}

func requireTerminal() error {
	if clitui.IsInteractive() {
		return nil
	}
	cause := errors.New("this command opens an editor and needs a terminal")
	return newPolicyError(usefulerror.ErrBadRequest, cause.Error(),
		"Run it in a terminal. To read the policy in a script, run: safedep policy get", cause)
}

func runEdit(
	ctx context.Context,
	client policyv1grpc.PolicyServiceClient,
	open openEditorFunc,
	tenant string,
	in editInput,
) (*policyv1.PolicyChange, error) {
	if err := checkMaxLength("-m", in.Title, &policyv1.CreatePolicyChangeRequest{}, "title"); err != nil {
		return nil, err
	}
	head, err := getPolicy(ctx, client)
	if err != nil {
		return nil, err
	}
	intro := []string{
		fmt.Sprintf("Policy of %s%shead revision %d", tenantName(tenant), sep, head.GetHeadRevision()),
		"Saving creates a draft change. Nothing applies until you publish it.",
	}
	header := editHeader(intro, head)

	var change *policyv1.PolicyChange
	err = editSettings(ctx, open, head.GetPolicy(), header, defaultLayer(head), in.Override, func(p *policyv1.Policy) error {
		req := &policyv1.CreatePolicyChangeRequest{}
		req.SetTitle(in.Title)
		req.SetSettings(p)
		req.SetBaseRevision(head.GetHeadRevision())
		res, err := client.CreatePolicyChange(ctx, req)
		if err != nil {
			return mapChangeError("create", "", err)
		}
		change = res.GetChange()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return change, nil
}

func tenantName(tenant string) string {
	if tenant == "" {
		return "this tenant"
	}
	return tenant
}

// editHeader is the comment block at the top of the document. It shows the
// layers, because the SafeDep default policy is below the policy base and
// the tenant cannot change it.
func editHeader(intro []string, head *policyv1.GetPolicyResponse) string {
	defaults := make([]string, 0)
	for _, f := range cloudpolicy.Fields(head.GetDefaultPolicy().GetSpec()) {
		defaults = append(defaults, f.Path+": "+f.Value)
	}
	if len(defaults) == 0 {
		defaults = append(defaults, "(no values)")
	}
	counts := head.GetPackageRuleCounts()
	rules := counts.GetTrustCount() + counts.GetBlockCount()

	lines := append([]string{}, intro...)
	lines = append(lines,
		"",
		"Layers, from low to high, for each subject:",
		fmt.Sprintf("  1. SafeDep default policy (read-only, revision %d):", head.GetDefaultPolicy().GetRevision()),
		"       "+strings.Join(defaults, sep),
		"  2. base      - applies to every endpoint and project of this tenant",
		"  3. overrides - apply to the subjects that their selector matches.",
		"                 For each field, the highest priority that sets it wins.",
		"",
		"Keep the id of each override. A new override has no id.",
		fmt.Sprintf("Package rules are not in this file: %d %s (safedep policy rule list)",
			rules, cloudpolicy.Plural(int(rules), "rule", "rules")),
		"The policy keeps no comments. Write the reason for a value in a description field.",
		"To set a field, remove the \"# \" at the start of its line and of the lines of its parents.",
		"Fields and values: "+settingsReferenceURL,
	)
	return cloudpolicy.CommentBlock(lines)
}
