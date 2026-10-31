package endpoint

import (
	"context"
	"fmt"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
)

// This file holds the policy verbs of the endpoint tree: `label set|delete`
// and `policy show`. An endpoint argument is a ULID, a unique ULID prefix,
// or a hostname that `safedep endpoint list` cached.

func labelCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "label",
		Short: "Set or delete the custom labels of endpoints",
		Long: "Set or delete the custom/ labels of endpoints. A policy override selects endpoints by their " +
			"labels. An admin writes only custom/ labels. The shared managed file sets managed/ labels, and " +
			"the tools report reported/ labels. A label change takes effect at the next policy fetch of " +
			"each endpoint.",
	}
	parent.AddCommand(labelSetCmd(a), labelDeleteCmd(a))
	return parent
}

func labelSetCmd(a *app.App) *cobra.Command {
	var labels []string
	cmd := &cobra.Command{
		Use:   "set ENDPOINT... --label custom/KEY=VALUE",
		Short: "Set custom labels on endpoints",
		Long: "Set custom/ labels on up to 100 endpoints. A label with an existing key gets the new value. " +
			"The label takes effect at the next policy fetch of each endpoint.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
				return err
			}
			_, err := cloudpolicy.ParseCustomLabels(labels)
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := cloudpolicy.ParseCustomLabels(labels)
			if err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			dir, err := NewDirectoryFromApp(a)
			if err != nil {
				return err
			}
			n, err := runLabelSet(cmd.Context(), cloudpolicy.NewLabelClient(client.Connection()), dir, args, parsed)
			if err != nil {
				return err
			}
			tui.Success("label set on %d %s. It takes effect at the next policy fetch of each endpoint",
				n, plural(n, "endpoint", "endpoints"))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&labels, "label", nil, "a label as custom/KEY=VALUE; repeatable")
	return cmd
}

func labelDeleteCmd(a *app.App) *cobra.Command {
	var keys []string
	cmd := &cobra.Command{
		Use:   "delete ENDPOINT... --key custom/KEY",
		Short: "Delete custom labels from endpoints",
		Long:  "Delete custom/ labels from up to 100 endpoints. The change takes effect at the next policy fetch of each endpoint.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
				return err
			}
			return cloudpolicy.ValidateCustomKeys(keys)
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
			n, err := runLabelDelete(cmd.Context(), cloudpolicy.NewLabelClient(client.Connection()), dir, args, keys)
			if err != nil {
				return err
			}
			tui.Success("label deleted from %d %s. It takes effect at the next policy fetch of each endpoint",
				n, plural(n, "endpoint", "endpoints"))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&keys, "key", nil, "a label key as custom/KEY; repeatable")
	return cmd
}

func resolveEndpoints(ctx context.Context, dir *Directory, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		id, err := dir.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("endpoint %q: %w", ref, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func runLabelSet(
	ctx context.Context,
	client policyv1grpc.LabelServiceClient,
	dir *Directory,
	refs []string,
	labels []*messagescontroltowerv1.Label,
) (int, error) {
	ids, err := resolveEndpoints(ctx, dir, refs)
	if err != nil {
		return 0, err
	}
	if err := cloudpolicy.SetLabels(ctx, client, specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, ids, labels); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func runLabelDelete(
	ctx context.Context,
	client policyv1grpc.LabelServiceClient,
	dir *Directory,
	refs []string,
	keys []string,
) (int, error) {
	ids, err := resolveEndpoints(ctx, dir, refs)
	if err != nil {
		return 0, err
	}
	if err := cloudpolicy.DeleteLabels(ctx, client, specv1.SubjectKind_SUBJECT_KIND_ENDPOINT, ids, keys); err != nil {
		return 0, err
	}
	return len(ids), nil
}

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
			"applied policy revision and the exceptions that each tool reported, such as a field that a " +
			"managed file locks. Use --field to show some fields only.",
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
	cmd.Flags().StringSliceVar(&fields, "field", nil, "show only these contract paths, as in package_admission.cooldown.min_age; repeatable")
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
