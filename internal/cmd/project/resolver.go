package project

import (
	"context"
	"fmt"
	"sort"
	"strings"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/tui"
)

const (
	projectLookupChunkSize = 10
	projectLookupPageSize  = 100
)

type listProjectsClient interface {
	ListProjects(
		context.Context,
		*controltowerv1.ListProjectsRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListProjectsResponse, error)
}

type projectMatch struct {
	id        string
	source    string
	originURL string
}

func newListProjectsClient(conn grpc.ClientConnInterface) listProjectsClient {
	return controltowerv1grpc.NewProjectServiceClient(conn)
}

func runCreate(
	ctx context.Context,
	scanner createProjectScansClient,
	resolver listProjectsClient,
	in createInput,
) (*createResult, error) {
	if err := validateCreateInput(in); err != nil {
		return nil, err
	}

	resolvedIDs, err := resolveProjectNames(ctx, resolver, in.ProjectNames)
	if err != nil {
		return nil, err
	}

	projectIDs := append([]string{}, in.ProjectIDs...)
	seen := make(map[string]struct{}, len(projectIDs)+len(resolvedIDs))
	for _, projectID := range projectIDs {
		seen[projectID] = struct{}{}
	}
	for _, projectID := range resolvedIDs {
		if _, duplicate := seen[projectID]; duplicate {
			cause := fmt.Errorf("duplicate project ID %q after name resolution", projectID)
			return nil, invalidProjectSelectionError(
				cause,
				fmt.Sprintf("Remove duplicate project ID %q and retry.", projectID),
			)
		}
		seen[projectID] = struct{}{}
		projectIDs = append(projectIDs, projectID)
	}

	return createProjectScans(ctx, scanner, projectIDs)
}

func resolveProjectNames(
	ctx context.Context,
	client listProjectsClient,
	projectNames []string,
) ([]string, error) {
	if len(projectNames) == 0 {
		return nil, nil
	}

	matches := make(map[string]map[string]projectMatch, len(projectNames))
	for start := 0; start < len(projectNames); start += projectLookupChunkSize {
		end := min(start+projectLookupChunkSize, len(projectNames))
		if err := resolveProjectNameChunk(ctx, client, projectNames[start:end], matches); err != nil {
			return nil, err
		}
	}

	resolved := make([]string, 0, len(projectNames))
	for _, name := range projectNames {
		byID := matches[name]
		switch len(byID) {
		case 0:
			cause := fmt.Errorf("project scan create: project %q not found", name)
			return nil, newProjectError(
				usefulerror.ErrNotFound,
				"Project not found",
				fmt.Sprintf("Check that project %q exists in the current tenant or retry with --project-id.", name),
				cause,
			)
		case 1:
			for id := range byID {
				resolved = append(resolved, id)
			}
		default:
			return nil, ambiguousProjectNameError(name, byID)
		}
	}
	return resolved, nil
}

func resolveProjectNameChunk(
	ctx context.Context,
	client listProjectsClient,
	names []string,
	matches map[string]map[string]projectMatch,
) error {
	requested := make(map[string]struct{}, len(names))
	for _, name := range names {
		requested[name] = struct{}{}
	}

	const label = "project scan create: resolve projects"
	return paginate(ctx, label, func(ctx context.Context, pageToken string) (string, error) {
		res, err := client.ListProjects(ctx, newListProjectsRequest(names, pageToken))
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		if err := collectProjectMatches(res, requested, matches); err != nil {
			return "", err
		}
		return res.GetPagination().GetNextPageToken(), nil
	})
}

func newListProjectsRequest(names []string, pageToken string) *controltowerv1.ListProjectsRequest {
	filter := &controltowerv1.ListProjectsRequest_Filter{}
	filter.SetProjectNames(names)
	req := &controltowerv1.ListProjectsRequest{}
	req.SetFilterV2(filter)
	req.SetPagination(newPaginationRequest(projectLookupPageSize, pageToken))
	return req
}

func collectProjectMatches(
	res *controltowerv1.ListProjectsResponse,
	requested map[string]struct{},
	matches map[string]map[string]projectMatch,
) error {
	for i, item := range res.GetProjects() {
		project := item.GetProject()
		if project == nil || project.GetProjectId() == "" || project.GetName() == "" {
			return fmt.Errorf(
				"project scan create: resolve projects: invalid response: project %d is missing identity",
				i+1,
			)
		}
		name := project.GetName()
		if _, ok := requested[name]; !ok {
			return fmt.Errorf(
				"project scan create: resolve projects: invalid response: unexpected project name %q",
				name,
			)
		}
		if matches[name] == nil {
			matches[name] = make(map[string]projectMatch)
		}
		matches[name][project.GetProjectId()] = projectMatch{
			id:        project.GetProjectId(),
			source:    tui.EnumToken(project.GetSource().String(), "SOURCE_"),
			originURL: project.GetOriginUrl(),
		}
	}
	return nil
}

func ambiguousProjectNameError(name string, byID map[string]projectMatch) error {
	items := make([]projectMatch, 0, len(byID))
	for _, match := range byID {
		items = append(items, match)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].id < items[j].id
	})

	labels := make([]string, 0, len(items))
	for _, item := range items {
		details := item.source
		if item.originURL != "" {
			details += ", " + item.originURL
		}
		labels = append(labels, fmt.Sprintf("%s (%s)", item.id, details))
	}
	cause := fmt.Errorf(
		"project scan create: project name %q is ambiguous: %s; use a project ID",
		name,
		strings.Join(labels, ", "),
	)
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Project name is ambiguous",
		"Retry with one of these project IDs: "+strings.Join(labels, ", ")+".",
		cause,
	)
}
