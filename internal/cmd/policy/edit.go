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
	"google.golang.org/protobuf/proto"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/config"
	clitui "github.com/safedep/cli/internal/tui"
)

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
			tui.Success("change %s saved from revision %d", change.GetChangeId(), change.GetBaseRevision())
			tui.Info("  Review it: safedep policy change show %s", change.GetChangeId())
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
	err = editSettings(ctx, open, head.GetPolicy(), header, in.Override, func(p *policyv1.Policy) error {
		req := &policyv1.CreatePolicyChangeRequest{}
		req.SetTitle(in.Title)
		req.SetSettings(p)
		req.SetBaseRevision(head.GetHeadRevision())
		res, err := client.CreatePolicyChange(ctx, req)
		if err != nil {
			return fmt.Errorf("policy change create: %w", err)
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
		fmt.Sprintf("Package rules are not in this file: %d %s (safedep policy rule list)", rules, plural(int(rules), "rule", "rules")),
		"The policy keeps no comments. Write the reason for a value in a description field.",
	)
	return cloudpolicy.CommentBlock(lines)
}

// editSettings opens the settings, or one policy override of them, in the
// editor and gives the whole edited document to commit.
func editSettings(
	ctx context.Context,
	open openEditorFunc,
	start *policyv1.Policy,
	header string,
	overrideName string,
	commit func(*policyv1.Policy) error,
) error {
	if start == nil {
		start = &policyv1.Policy{}
	}
	if overrideName == "" {
		text, err := cloudpolicy.MarshalPolicy(start, header)
		if err != nil {
			return err
		}
		return editLoop(ctx, open, text, func(saved []byte) error {
			edited, err := cloudpolicy.UnmarshalPolicy(saved)
			if err != nil {
				return retryable(err)
			}
			cloudpolicy.RestoreOverrideIDs(edited, start)
			if err := validateSettings(edited); err != nil {
				return retryable(err)
			}
			warnComments(saved, header)
			return commit(edited)
		})
	}

	current, err := findOverride(start, overrideName)
	if err != nil {
		return err
	}
	text, err := cloudpolicy.MarshalOverride(current, header)
	if err != nil {
		return err
	}
	return editLoop(ctx, open, text, func(saved []byte) error {
		edited, err := cloudpolicy.UnmarshalOverride(saved)
		if err != nil {
			return retryable(err)
		}
		if edited.GetOverrideId() == "" {
			edited.SetOverrideId(current.GetOverrideId())
		}
		full, ok := proto.Clone(start).(*policyv1.Policy)
		if !ok {
			return errors.New("policy edit: clone the policy")
		}
		overrides := full.GetOverrides()
		for i, o := range overrides {
			if o.GetOverrideId() == current.GetOverrideId() {
				overrides[i] = edited
			}
		}
		full.SetOverrides(overrides)
		if err := validateSettings(full); err != nil {
			return retryable(err)
		}
		warnComments(saved, header)
		return commit(full)
	})
}

// validateSettings checks the rules that a person breaks most often in an
// edit, so the editor shows them before the RPC. The server checks every
// rule.
func validateSettings(p *policyv1.Policy) error {
	names := map[string]struct{}{}
	priorities := map[uint32]string{}
	for i, o := range p.GetOverrides() {
		if o.GetName() == "" {
			return fmt.Errorf("overrides[%d]: give the override a name", i)
		}
		if _, dup := names[o.GetName()]; dup {
			return fmt.Errorf("overrides[%d]: two overrides are named %q. Each name must be unique", i, o.GetName())
		}
		names[o.GetName()] = struct{}{}
		if o.GetPriority() == 0 {
			return fmt.Errorf("override %q: give it a priority from 1 to 1000000", o.GetName())
		}
		if other, dup := priorities[o.GetPriority()]; dup {
			return fmt.Errorf("override %q: priority %d is also the priority of %q. Each priority must be unique",
				o.GetName(), o.GetPriority(), other)
		}
		priorities[o.GetPriority()] = o.GetName()
		if !o.HasSelector() {
			return fmt.Errorf("override %q: give it a selector. Write selector: {} to select every subject", o.GetName())
		}
	}
	return nil
}

func warnComments(saved []byte, header string) {
	if cloudpolicy.HasUserComments(saved, header) {
		tui.Warning("The file holds comments. The policy keeps no comments, so they are lost. " +
			"Write the reason in a description field")
	}
}
