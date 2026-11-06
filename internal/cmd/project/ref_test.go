package project

import (
	"context"
	"strings"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func twoQueuedScans() *fakeCreateProjectScansClient {
	return &fakeCreateProjectScansClient{
		res: createResponse(
			projectScan("project-1", "session-1", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{}),
			projectScan("project-2", "session-2", messagescontroltowerv1.ScanStatus_SCAN_STATUS_QUEUED, time.Time{}),
		),
	}
}

func TestRunCreate_SendsTheRefForEveryProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "plain name is a branch", ref: "main", want: "refs/heads/main"},
		{name: "plain name with a slash is a branch", ref: "release/1.2", want: "refs/heads/release/1.2"},
		{name: "plain name that looks like a tag is still a branch", ref: "v1.2.0", want: "refs/heads/v1.2.0"},
		{name: "full branch name", ref: "refs/heads/develop", want: "refs/heads/develop"},
		{name: "full tag name", ref: "refs/tags/v1.2.0", want: "refs/tags/v1.2.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scanner := twoQueuedScans()

			_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
				ProjectIDs: []string{"project-1", "project-2"},
				Ref:        tt.ref,
				RefSet:     true,
			})

			require.NoError(t, err)
			require.Len(t, scanner.req.GetTargets(), 2)
			for _, target := range scanner.req.GetTargets() {
				require.True(t, target.HasRef(), target.GetProjectId())
				assert.Equal(t, tt.want, target.GetRef().GetRef(), target.GetProjectId())
			}
		})
	}
}

func TestRunCreate_WithoutRefScansTheDefaultBranch(t *testing.T) {
	t.Parallel()
	scanner := twoQueuedScans()

	_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
		ProjectIDs: []string{"project-1", "project-2"},
	})

	require.NoError(t, err)
	require.Len(t, scanner.req.GetTargets(), 2)
	for _, target := range scanner.req.GetTargets() {
		assert.False(t, target.HasRef(), target.GetProjectId())
	}
}

func TestCreateCmd_RejectsAnEmptyRefBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	cmd := createCmd(nil)
	require.NoError(t, cmd.ParseFlags([]string{"--ref", "", "--project-id", "project-1"}))

	err := cmd.Args(cmd, cmd.Flags().Args())

	requireUsefulError(
		t,
		err,
		usefulerror.ErrBadRequest,
		"Invalid ref",
		"Pass a branch name, refs/heads/<branch> or refs/tags/<tag>, or omit --ref to scan the default branch.",
	)
}

func TestRunCreate_ExplainsARefTheServerRejects(t *testing.T) {
	t.Parallel()

	serverMessage := "project project-2 cannot scan refs/heads/release/1.2: git ref not found"
	scanner := &fakeCreateProjectScansClient{err: status.Error(codes.InvalidArgument, serverMessage)}

	_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
		ProjectIDs: []string{"project-1", "project-2"},
		Ref:        "release/1.2",
		RefSet:     true,
	})

	requireUsefulError(
		t,
		err,
		usefulerror.ErrBadRequest,
		"Ref cannot be scanned",
		serverMessage+". Check that refs/heads/release/1.2 exists in every selected repository. "+
			"A plain name is a branch: pass refs/tags/<tag> for a tag.",
	)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRunCreate_DoesNotBlameARefThatWasNotRequested(t *testing.T) {
	t.Parallel()

	scanner := &fakeCreateProjectScansClient{err: status.Error(codes.InvalidArgument, "at least one target is required")}

	_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
		ProjectIDs: []string{"project-1"},
	})

	require.Error(t, err)
	usefulErr, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.NotEqual(t, "Ref cannot be scanned", usefulErr.HumanError())
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRunCreate_KeepsOtherServerErrorsWhenARefIsSet(t *testing.T) {
	t.Parallel()

	rejection := usefulerror.NewStatus(codes.FailedPrecondition, "service execution failed").
		WithReason(errorv1.ErrorReason_ERROR_REASON_PROJECT_NOT_SCANNABLE).
		Err()
	scanner := &fakeCreateProjectScansClient{err: rejection}

	_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
		ProjectIDs: []string{"project-1"},
		Ref:        "main",
		RefSet:     true,
	})

	require.Error(t, err)
	usefulErr, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.NotEqual(t, "Ref cannot be scanned", usefulErr.HumanError())
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	reason, ok := usefulerror.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, errorv1.ErrorReason_ERROR_REASON_PROJECT_NOT_SCANNABLE, reason)
}

// FuzzNewCreateRequest_Ref checks the rule that maps any --ref value to the
// ref the API receives: a value under refs/ is sent as given, and any other
// value names a branch.
func FuzzNewCreateRequest_Ref(f *testing.F) {
	for _, seed := range []string{
		"main", "release/1.2", "refs/heads/main", "refs/tags/v1.2.0", "refs/pull/1/head",
		"refs/", "refs", "heads/main", "tags/v1", " refs/heads/main", "REFS/heads/main",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, ref string) {
		if ref == "" {
			return
		}

		req := newCreateRequest([]string{"project-1"}, ref)

		got := req.GetTargets()[0].GetRef().GetRef()
		if strings.HasPrefix(ref, "refs/") {
			assert.Equal(t, ref, got)
		} else {
			assert.Equal(t, "refs/heads/"+ref, got)
		}
	})
}
