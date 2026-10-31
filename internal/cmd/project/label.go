package project

import (
	"context"
	"errors"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
)

type labelInput struct {
	Names      []string
	ProjectIDs []string
	Labels     []string
	Keys       []string
}

func labelCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "label",
		Short: "Set or delete the custom labels of projects",
		Long: "Set or delete the custom/ labels of projects. A policy override selects projects by their " +
			"labels. An admin writes only custom/ labels. The forge sync writes scm/ labels, and the server " +
			"writes safedep.io/ labels. A label change takes effect at the next check of each project.",
	}
	parent.AddCommand(labelSetCmd(a), labelDeleteCmd(a))
	return parent
}

func labelSetCmd(a *app.App) *cobra.Command {
	var in labelInput
	cmd := &cobra.Command{
		Use:   "set [PROJECT_NAME...] --label custom/KEY=VALUE",
		Short: "Set custom labels on projects",
		Long: "Set custom/ labels on up to 100 projects, by exact name or with --project-id. A label with " +
			"an existing key gets the new value. The label takes effect at the next check of each project.",
		Args: func(_ *cobra.Command, args []string) error {
			in.Names = args
			if err := requireProjects(in); err != nil {
				return err
			}
			_, err := cloudpolicy.ParseCustomLabels(in.Labels)
			return labelError(err)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Names = args
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			conn := client.Connection()
			n, err := runLabelSet(cmd.Context(), newListProjectsClient(conn), cloudpolicy.NewLabelClient(conn), in)
			if err != nil {
				return err
			}
			tui.Success("label set on %d %s. It takes effect at the next check of each project", n, pluralProjects(n))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&in.Labels, "label", nil, "a label as custom/KEY=VALUE; repeatable")
	cmd.Flags().StringArrayVar(&in.ProjectIDs, "project-id", nil, "a project ID instead of a name; repeatable")
	return cmd
}

func labelDeleteCmd(a *app.App) *cobra.Command {
	var in labelInput
	cmd := &cobra.Command{
		Use:   "delete [PROJECT_NAME...] --key custom/KEY",
		Short: "Delete custom labels from projects",
		Long: "Delete custom/ labels from up to 100 projects, by exact name or with --project-id. The " +
			"change takes effect at the next check of each project.",
		Args: func(_ *cobra.Command, args []string) error {
			in.Names = args
			if err := requireProjects(in); err != nil {
				return err
			}
			return labelError(cloudpolicy.ValidateCustomKeys(in.Keys))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Names = args
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			conn := client.Connection()
			n, err := runLabelDelete(cmd.Context(), newListProjectsClient(conn), cloudpolicy.NewLabelClient(conn), in)
			if err != nil {
				return err
			}
			tui.Success("label deleted from %d %s. It takes effect at the next check of each project", n, pluralProjects(n))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&in.Keys, "key", nil, "a label key as custom/KEY; repeatable")
	cmd.Flags().StringArrayVar(&in.ProjectIDs, "project-id", nil, "a project ID instead of a name; repeatable")
	return cmd
}

func requireProjects(in labelInput) error {
	if len(in.Names)+len(in.ProjectIDs) == 0 {
		return invalidProjectSelectionError(errors.New("give at least one project"),
			"Give project names, or --project-id. Find them with: safedep project list")
	}
	if err := validateUniqueValues(in.ProjectIDs, "project ID"); err != nil {
		return err
	}
	return validateUniqueValues(in.Names, "project name")
}

func labelError(err error) error {
	if err == nil {
		return nil
	}
	return newProjectError(usefulerror.ErrBadRequest, err.Error(), "Write a label as custom/KEY=VALUE, as in custom/team=release", err)
}

// labelProjectIDs resolves the names and adds the given project IDs.
func labelProjectIDs(ctx context.Context, projects listProjectsClient, op string, in labelInput) ([]string, error) {
	resolved, err := resolveProjectNamesFor(ctx, projects, op, in.Names)
	if err != nil {
		return nil, err
	}
	return append(append([]string{}, in.ProjectIDs...), resolved...), nil
}

func runLabelSet(
	ctx context.Context,
	projects listProjectsClient,
	labels policyv1grpc.LabelServiceClient,
	in labelInput,
) (int, error) {
	parsed, err := cloudpolicy.ParseCustomLabels(in.Labels)
	if err != nil {
		return 0, labelError(err)
	}
	ids, err := labelProjectIDs(ctx, projects, "project label set", in)
	if err != nil {
		return 0, err
	}
	if err := cloudpolicy.SetLabels(ctx, labels, specv1.SubjectKind_SUBJECT_KIND_PROJECT, ids, parsed); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func runLabelDelete(
	ctx context.Context,
	projects listProjectsClient,
	labels policyv1grpc.LabelServiceClient,
	in labelInput,
) (int, error) {
	if err := cloudpolicy.ValidateCustomKeys(in.Keys); err != nil {
		return 0, labelError(err)
	}
	ids, err := labelProjectIDs(ctx, projects, "project label delete", in)
	if err != nil {
		return 0, err
	}
	if err := cloudpolicy.DeleteLabels(ctx, labels, specv1.SubjectKind_SUBJECT_KIND_PROJECT, ids, in.Keys); err != nil {
		return 0, err
	}
	return len(ids), nil
}
