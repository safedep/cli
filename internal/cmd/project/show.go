package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

type projectShowResult struct {
	Project projectView                `json:"project"`
	Policy  *cloudpolicy.SubjectPolicy `json:"-"`
}

type showInput struct {
	Name      string
	ProjectID string
}

func showCmd(a *app.App) *cobra.Command {
	var in showInput
	cmd := &cobra.Command{
		Use:   "show [PROJECT_NAME]",
		Short: "Show one project with its labels and policy",
		Long: "Show one project by its exact name, or by --project-id when two projects have the name: " +
			"its source, its labels from every source, and the policy revision and policy overrides " +
			"that apply to it. A repository is a project.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			return requireOneProject(args, in.ProjectID, "As in: safedep project show acme/payments-api")
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
			res, err := runShow(cmd.Context(), newListProjectsClient(conn), cloudpolicy.NewPolicyClient(conn), in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().StringVar(&in.ProjectID, "project-id", "", "the project ID instead of a name")
	return cmd
}

// requireOneProject checks that the command line names one project, by a
// name or by --project-id.
func requireOneProject(args []string, projectID, help string) error {
	if (len(args) == 1) == (projectID != "") {
		return invalidProjectSelectionError(errors.New("give a project name or --project-id"), help)
	}
	return nil
}

// projectIDLabel is the label that the server sets on each project. The
// ListProjects filter has no project ID field, so a read by ID selects on it.
const projectIDLabel = "safedep.io/project-id"

// findProjectByID reads one project by its ID.
func findProjectByID(ctx context.Context, client listProjectsClient, op, id string) (*controltowerv1.ProjectWithAttributes, error) {
	notFound := newProjectError(usefulerror.ErrNotFound, "Project not found",
		"Find the project ID with: safedep project list",
		fmt.Errorf("%s: project ID %q not found", op, id))
	selector, err := cloudpolicy.ParseSelector(projectIDLabel + "=" + id)
	if err != nil {
		return nil, notFound
	}
	filter := &controltowerv1.ListProjectsRequest_Filter{}
	filter.SetLabelSelector(selector)
	req := &controltowerv1.ListProjectsRequest{}
	req.SetFilterV2(filter)
	res, err := client.ListProjects(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	for _, p := range res.GetProjects() {
		if p.GetProject().GetProjectId() == id {
			return p, nil
		}
	}
	return nil, notFound
}

// findProject reads one project by its exact name.
func findProject(ctx context.Context, client listProjectsClient, op, name string) (*controltowerv1.ProjectWithAttributes, error) {
	filter := &controltowerv1.ListProjectsRequest_Filter{}
	filter.SetProjectNames([]string{name})
	req := &controltowerv1.ListProjectsRequest{}
	req.SetFilterV2(filter)
	res, err := client.ListProjects(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	var found []*controltowerv1.ProjectWithAttributes
	byID := map[string]projectMatch{}
	for _, p := range res.GetProjects() {
		if p.GetProject().GetName() != name {
			continue
		}
		found = append(found, p)
		byID[p.GetProject().GetProjectId()] = newProjectMatch(p.GetProject())
	}
	switch len(found) {
	case 0:
		return nil, newProjectError(usefulerror.ErrNotFound, "Project not found",
			fmt.Sprintf("Check the name with: safedep project list --name %q", name),
			fmt.Errorf("%s: project %q not found", op, name))
	case 1:
		return found[0], nil
	default:
		return nil, ambiguousProjectNameError(op, name, byID)
	}
}

func runShow(
	ctx context.Context,
	projects listProjectsClient,
	policies cloudpolicy.SubjectPolicyClient,
	in showInput,
) (*projectShowResult, error) {
	var project *controltowerv1.ProjectWithAttributes
	var err error
	if in.ProjectID != "" {
		project, err = findProjectByID(ctx, projects, "project show", in.ProjectID)
	} else {
		project, err = findProject(ctx, projects, "project show", in.Name)
	}
	if err != nil {
		return nil, err
	}
	out := &projectShowResult{Project: newProjectView(project)}
	name := out.Project.Name
	subject := cloudpolicy.NewSubject(specv1.SubjectKind_SUBJECT_KIND_PROJECT, out.Project.ID)
	policy, err := cloudpolicy.GetSubjectPolicy(ctx, policies, subject, name, nil)
	if err != nil {
		// The project data is still useful when the policy service does not
		// answer, so the command warns and shows the rest.
		tui.Warning("Cannot read the policy of project %s: %s", clitui.EscapeLine(name), clitui.EscapeLine(err.Error()))
		return out, nil
	}
	out.Policy = policy
	return out, nil
}

func (r *projectShowResult) RenderJSON() ([]byte, error) {
	type policyJSON struct {
		Revision        uint64                        `json:"revision"`
		DefaultRevision uint64                        `json:"default_policy_revision"`
		Matched         []cloudpolicy.MatchedOverride `json:"matched_overrides"`
	}
	out := struct {
		projectView
		Policy *policyJSON `json:"policy,omitempty"`
	}{projectView: r.Project}
	if p := r.Policy; p != nil {
		matched := p.Matched
		if matched == nil {
			matched = []cloudpolicy.MatchedOverride{}
		}
		out.Policy = &policyJSON{Revision: p.Revision, DefaultRevision: p.DefaultRevision, Matched: matched}
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *projectShowResult) policyLine() string {
	if r.Policy == nil {
		return "unknown: the policy service did not answer"
	}
	return r.Policy.PolicyLine()
}

func (r *projectShowResult) RenderTable() string {
	p := r.Project
	source := p.Source
	if p.OriginURL != "" {
		source += cloudpolicy.Separator + p.OriginURL
	}
	created := "-"
	if p.created != nil {
		created = humanize.Time(*p.created, time.Now())
	}
	rows := [][2]string{
		{"Project", p.Name},
		{"ID", p.ID},
		{"Source", source},
	}
	labels := p.Labels
	if len(labels) == 0 {
		labels = []string{"-"}
	}
	for i, l := range labels {
		key := ""
		if i == 0 {
			key = "Labels"
		}
		rows = append(rows, [2]string{key, l})
	}
	rows = append(rows, [][2]string{
		{"Policy", r.policyLine()},
		{"Vulnerabilities", countCell(p.Vulnerabilities)},
		{"Violations", countCell(p.PolicyViolations)},
		{"Created", created},
	}...)
	return cloudpolicy.KeyValues(rows) + "\n\nPolicy of each field: safedep project policy show " + clitui.EscapeLine(p.Name)
}

func (r *projectShowResult) RenderPlain() string {
	p := r.Project
	policy := ""
	if r.Policy != nil {
		policy = fmt.Sprintf("%d", r.Policy.Revision)
	}
	return strings.Join([]string{
		clitui.PlainRow("project_id", p.ID),
		clitui.PlainRow("name", p.Name),
		clitui.PlainRow("source", p.Source),
		clitui.PlainRow("origin_url", p.OriginURL),
		clitui.PlainRow("labels", strings.Join(p.Labels, ",")),
		clitui.PlainRow("policy_revision", policy),
		clitui.PlainRow("created_at", p.CreatedAt),
	}, "\n")
}
