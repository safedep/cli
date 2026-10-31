package project

import (
	"context"
	"errors"

	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
)

type policyShowInput struct {
	Name      string
	ProjectID string
	Fields    []string
}

func policyCmd(a *app.App) *cobra.Command {
	parent := &cobra.Command{
		Use:   "policy",
		Short: "Inspect the policy of a project",
		Long:  "Inspect the policy that applies to a project, with the source of each value.",
	}
	parent.AddCommand(policyShowCmd(a))
	return parent
}

func policyShowCmd(a *app.App) *cobra.Command {
	var in policyShowInput
	cmd := &cobra.Command{
		Use:   "show [PROJECT_NAME]",
		Short: "Show the policy of a project and the source of each value",
		Long: "Show the intended policy of one project: each field, its value, and its source, which is " +
			"the SafeDep default policy, the policy base, or a policy override. The output also shows the " +
			"exceptions and the applied policy revision that each tool or check reported. Use --field to " +
			"show some fields only.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if (len(args) == 1) == (in.ProjectID != "") {
				return invalidProjectSelectionError(errors.New("give a project name or --project-id"),
					"As in: safedep project policy show acme/payments-api")
			}
			return cloudpolicy.ValidateFieldPaths(in.Fields)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				in.Name = args[0]
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			conn := client.Connection()
			res, err := runPolicyShow(cmd.Context(), newListProjectsClient(conn), cloudpolicy.NewPolicyClient(conn), in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().StringVar(&in.ProjectID, "project-id", "", "the project ID instead of a name")
	cmd.Flags().StringSliceVar(&in.Fields, "field", nil, "show only these contract paths, as in scan_gate.fail_on; repeatable")
	return cmd
}

func runPolicyShow(
	ctx context.Context,
	projects listProjectsClient,
	policies cloudpolicy.SubjectPolicyClient,
	in policyShowInput,
) (*cloudpolicy.SubjectPolicy, error) {
	id := in.ProjectID
	if id == "" {
		project, err := findProject(ctx, projects, "project policy show", in.Name)
		if err != nil {
			return nil, err
		}
		id = project.GetProject().GetProjectId()
	}
	subject := cloudpolicy.NewSubject(specv1.SubjectKind_SUBJECT_KIND_PROJECT, id)
	return cloudpolicy.GetSubjectPolicy(ctx, policies, subject, in.Name, in.Fields)
}
