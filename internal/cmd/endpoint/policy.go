package endpoint

import (
	"context"

	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
)

// This file holds `endpoint policy show`. label.go holds `label set|delete`.
// An endpoint argument is a ULID, a unique ULID prefix, or a hostname that
// `safedep endpoint list` cached.

func policyCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "policy",
		Short: "Inspect the policy of an endpoint",
		Long:  "Inspect the policy that applies to an endpoint, with the source of each value and what each tool applied.",
	}
	parent.AddCommand(policyShowCmd(a))
	return parent
}

func policyShowCmd(a *app.App) *cobra.Command {
	var fields []string
	cmd := &cobra.Command{
		Use:   "show ENDPOINT",
		Short: "Show the policy of an endpoint and the source of each value",
		Long: "Show the intended policy of one endpoint: each field, its value, and its source, which is " +
			"the SafeDep default policy, the policy base, or a policy override. The output also shows the " +
			"applied policy revision that each tool reported, and the fields that a tool did not apply " +
			"as the policy says. Use --field to show some fields only.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			return cloudpolicy.ValidateFieldPaths(fields)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			dir, err := NewDirectoryFromApp(a)
			if err != nil {
				return err
			}
			res, err := runPolicyShow(cmd.Context(), cloudpolicy.NewPolicyClient(client.Connection()), dir, args[0], fields)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().StringSliceVar(&fields, "field", nil, "show only this contract path and the paths below it, as in package_admission.cooldown.min_age (repeatable)")
	return cmd
}

func runPolicyShow(
	ctx context.Context,
	client cloudpolicy.SubjectPolicyClient,
	dir *Directory,
	ref string,
	fields []string,
) (*cloudpolicy.SubjectPolicy, error) {
	id, err := dir.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	name := ref
	if entry, ok := dir.Lookup(ctx, id); ok && entry.Hostname != "" {
		name = entry.Hostname
	}
	subject := cloudpolicy.NewSubject(specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, id)
	return cloudpolicy.GetSubjectPolicy(ctx, client, subject, name, fields)
}
