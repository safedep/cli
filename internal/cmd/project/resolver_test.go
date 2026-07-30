package project

import (
	"context"
	"testing"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeListProjectsClient struct {
	requests []*controltowerv1.ListProjectsRequest
	listFn   func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error)
}

func (f *fakeListProjectsClient) ListProjects(
	_ context.Context,
	req *controltowerv1.ListProjectsRequest,
	_ ...grpc.CallOption,
) (*controltowerv1.ListProjectsResponse, error) {
	f.requests = append(f.requests, req)
	return f.listFn(req)
}

var (
	_ listProjectsClient = (*fakeListProjectsClient)(nil)
	_ listProjectsClient = controltowerv1grpc.ProjectServiceClient(nil)
)

func TestValidateCreateInput(t *testing.T) {
	t.Parallel()

	oneHundredNames := make([]string, maxProjectScans)
	for i := range oneHundredNames {
		oneHundredNames[i] = "project-" + string(rune('a'+i))
	}

	tests := []struct {
		name    string
		in      createInput
		wantErr string
	}{
		{name: "ID only", in: createInput{ProjectIDs: []string{"id-1"}}},
		{name: "name only", in: createInput{ProjectNames: []string{"safedep/cli"}}},
		{
			name: "mixed",
			in: createInput{
				ProjectIDs:   []string{"id-1"},
				ProjectNames: []string{"safedep/cli"},
			},
		},
		{name: "one hundred names", in: createInput{ProjectNames: oneHundredNames}},
		{name: "zero", wantErr: "between 1 and 100"},
		{
			name: "combined over limit",
			in: createInput{
				ProjectIDs:   []string{"id-1"},
				ProjectNames: oneHundredNames,
			},
			wantErr: "between 1 and 100",
		},
		{
			name:    "empty name",
			in:      createInput{ProjectNames: []string{""}},
			wantErr: "project name at position 1 must not be empty",
		},
		{
			name:    "duplicate name",
			in:      createInput{ProjectNames: []string{"safedep/cli", "safedep/cli"}},
			wantErr: `duplicate project name "safedep/cli"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCreateInput(tt.in)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestResolveProjectNames_UsesExactSourceNeutralFilter(t *testing.T) {
	t.Parallel()

	client := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return listProjectsResponse(
				project("id-cli", "safedep/cli", messagescontroltowerv1.Project_SOURCE_GITHUB, "https://github.com/safedep/cli"),
			), nil
		},
	}

	ids, err := resolveProjectNames(context.Background(), client, []string{"safedep/cli"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id-cli"}, ids)

	require.Len(t, client.requests, 1)
	filter := client.requests[0].GetFilterV2()
	require.NotNil(t, filter)
	assert.Equal(t, []string{"safedep/cli"}, filter.GetProjectNames())
	assert.Empty(t, filter.GetSources())
	assert.Equal(t, uint32(projectLookupPageSize), client.requests[0].GetPagination().GetPageSize())
	assert.Empty(t, client.requests[0].GetPagination().GetPageToken())
}

func TestResolveProjectNames_PreservesRequestedNameOrder(t *testing.T) {
	t.Parallel()

	client := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return listProjectsResponse(
				project("id-two", "two", messagescontroltowerv1.Project_SOURCE_GITHUB, ""),
				project("id-one", "one", messagescontroltowerv1.Project_SOURCE_GITHUB, ""),
			), nil
		},
	}

	ids, err := resolveProjectNames(context.Background(), client, []string{"one", "two"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id-one", "id-two"}, ids)
}

func TestResolveProjectNames_ChunksTenNamesPerRequest(t *testing.T) {
	t.Parallel()

	names := make([]string, projectLookupChunkSize+1)
	for i := range names {
		names[i] = "name-" + string(rune('a'+i))
	}

	client := &fakeListProjectsClient{
		listFn: func(req *controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			projects := make([]*controltowerv1.ProjectWithAttributes, 0, len(req.GetFilterV2().GetProjectNames()))
			for _, name := range req.GetFilterV2().GetProjectNames() {
				projects = append(projects, project("id-"+name, name, messagescontroltowerv1.Project_SOURCE_GITHUB, ""))
			}
			return listProjectsResponse(projects...), nil
		},
	}

	ids, err := resolveProjectNames(context.Background(), client, names)
	require.NoError(t, err)
	require.Len(t, ids, len(names))
	require.Len(t, client.requests, 2)
	assert.Len(t, client.requests[0].GetFilterV2().GetProjectNames(), projectLookupChunkSize)
	assert.Len(t, client.requests[1].GetFilterV2().GetProjectNames(), 1)
}

func TestResolveProjectNames_FollowsPagination(t *testing.T) {
	t.Parallel()

	client := &fakeListProjectsClient{
		listFn: func(req *controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			if req.GetPagination().GetPageToken() == "" {
				return listProjectsResponseWithNext(
					"next",
					project("id-one", "one", messagescontroltowerv1.Project_SOURCE_GITHUB, ""),
				), nil
			}
			return listProjectsResponse(
				project("id-two", "two", messagescontroltowerv1.Project_SOURCE_GITHUB, ""),
			), nil
		},
	}

	ids, err := resolveProjectNames(context.Background(), client, []string{"one", "two"})
	require.NoError(t, err)
	assert.Equal(t, []string{"id-one", "id-two"}, ids)
	require.Len(t, client.requests, 2)
	assert.Equal(t, "next", client.requests[1].GetPagination().GetPageToken())
}

func TestResolveProjectNames_RejectsMissingName(t *testing.T) {
	t.Parallel()

	client := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return listProjectsResponse(), nil
		},
	}

	_, err := resolveProjectNames(context.Background(), client, []string{"missing"})
	require.Error(t, err)
	assert.EqualError(t, err, `project scan create: project "missing" not found`)
}

func TestResolveProjectNames_RejectsAmbiguousName(t *testing.T) {
	t.Parallel()

	client := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return listProjectsResponse(
				project("id-github", "shared", messagescontroltowerv1.Project_SOURCE_GITHUB, "https://github.com/acme/shared"),
				project("id-gitlab", "shared", messagescontroltowerv1.Project_SOURCE_GITLAB, "https://gitlab.com/acme/shared"),
			), nil
		},
	}

	_, err := resolveProjectNames(context.Background(), client, []string{"shared"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `project name "shared" is ambiguous`)
	assert.Contains(t, err.Error(), "id-github (github, https://github.com/acme/shared)")
	assert.Contains(t, err.Error(), "id-gitlab (gitlab, https://gitlab.com/acme/shared)")
}

func TestResolveProjectNames_PreservesGRPCStatus(t *testing.T) {
	t.Parallel()

	client := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "denied")
		},
	}

	_, err := resolveProjectNames(context.Background(), client, []string{"safedep/cli"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project scan create: resolve projects")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestRunCreate_ResolvesMixedTargetsBeforeAdmission(t *testing.T) {
	t.Parallel()

	resolver := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return listProjectsResponse(
				project("resolved-id", "safedep/cli", messagescontroltowerv1.Project_SOURCE_GITHUB, ""),
			), nil
		},
	}
	scanner := &fakeCreateProjectScansClient{
		res: createResponse(
			projectScan("literal-id", "session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{}),
			projectScan("resolved-id", "session-2", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{}),
		),
	}

	_, err := runCreate(context.Background(), scanner, resolver, createInput{
		ProjectIDs:   []string{"literal-id"},
		ProjectNames: []string{"safedep/cli"},
	})
	require.NoError(t, err)
	require.Len(t, scanner.req.GetTargets(), 2)
	assert.Equal(t, "literal-id", scanner.req.GetTargets()[0].GetProjectId())
	assert.Equal(t, "resolved-id", scanner.req.GetTargets()[1].GetProjectId())
}

func TestRunCreate_RejectsCanonicalDuplicateBeforeAdmission(t *testing.T) {
	t.Parallel()

	resolver := &fakeListProjectsClient{
		listFn: func(*controltowerv1.ListProjectsRequest) (*controltowerv1.ListProjectsResponse, error) {
			return listProjectsResponse(
				project("same-id", "safedep/cli", messagescontroltowerv1.Project_SOURCE_GITHUB, ""),
			), nil
		},
	}
	scanner := &fakeCreateProjectScansClient{}

	_, err := runCreate(context.Background(), scanner, resolver, createInput{
		ProjectIDs:   []string{"same-id"},
		ProjectNames: []string{"safedep/cli"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate project ID "same-id" after name resolution`)
	assert.Zero(t, scanner.calls)
}

func listProjectsResponse(projects ...*controltowerv1.ProjectWithAttributes) *controltowerv1.ListProjectsResponse {
	return listProjectsResponseWithNext("", projects...)
}

func listProjectsResponseWithNext(
	next string,
	projects ...*controltowerv1.ProjectWithAttributes,
) *controltowerv1.ListProjectsResponse {
	pagination := &messagescontroltowerv1.PaginationResponse{}
	pagination.SetNextPageToken(next)
	response := &controltowerv1.ListProjectsResponse{}
	response.SetProjects(projects)
	response.SetPagination(pagination)
	return response
}

func project(
	id string,
	name string,
	source messagescontroltowerv1.Project_Source,
	originURL string,
) *controltowerv1.ProjectWithAttributes {
	item := &messagescontroltowerv1.Project{}
	item.SetProjectId(id)
	item.SetName(name)
	item.SetSource(source)
	item.SetOriginUrl(originURL)
	project := &controltowerv1.ProjectWithAttributes{}
	project.SetProject(item)
	return project
}
