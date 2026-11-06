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
		refs refFlags
		want string
	}{
		{
			name: "branch",
			refs: refFlags{Branch: "release/1.2", Set: []string{branchFlag}},
			want: "refs/heads/release/1.2",
		},
		{name: "tag", refs: refFlags{Tag: "v1.2.0", Set: []string{tagFlag}}, want: "refs/tags/v1.2.0"},
		{name: "plain ref is a branch", refs: refFlags{Ref: "main", Set: []string{refFlag}}, want: "refs/heads/main"},
		{
			name: "plain ref that looks like a tag is still a branch",
			refs: refFlags{Ref: "v1.2.0", Set: []string{refFlag}},
			want: "refs/heads/v1.2.0",
		},
		{
			name: "full branch ref",
			refs: refFlags{Ref: "refs/heads/develop", Set: []string{refFlag}},
			want: "refs/heads/develop",
		},
		{
			name: "full tag ref",
			refs: refFlags{Ref: "refs/tags/v1.2.0", Set: []string{refFlag}},
			want: "refs/tags/v1.2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scanner := twoQueuedScans()

			_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
				ProjectIDs: []string{"project-1", "project-2"},
				Refs:       tt.refs,
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

func TestCreateCmd_RejectsABadRefSelectionBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    []string
		wantHelp string
	}{
		{
			name:     "empty ref",
			flags:    []string{"--ref", ""},
			wantHelp: "Pass a branch name, refs/heads/<branch> or refs/tags/<tag>, or omit --ref to scan the default branch.",
		},
		{
			name:     "empty branch",
			flags:    []string{"--branch", ""},
			wantHelp: "Pass a branch name, or omit --branch to scan the default branch.",
		},
		{
			name:     "empty tag",
			flags:    []string{"--tag", ""},
			wantHelp: "Pass a tag name, or omit --tag to scan the default branch.",
		},
		{
			name:     "full ref given to branch",
			flags:    []string{"--branch", "refs/heads/main"},
			wantHelp: "Pass the name without refs/, or pass the full ref with --ref refs/heads/main.",
		},
		{
			name:     "full ref given to tag",
			flags:    []string{"--tag", "refs/tags/v1"},
			wantHelp: "Pass the name without refs/, or pass the full ref with --ref refs/tags/v1.",
		},
		{
			name:     "branch and tag",
			flags:    []string{"--branch", "main", "--tag", "v1"},
			wantHelp: "Pass only one of --branch, --tag or --ref.",
		},
		{
			name:     "ref and branch",
			flags:    []string{"--ref", "refs/heads/main", "--branch", "main"},
			wantHelp: "Pass only one of --branch, --tag or --ref.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := createCmd(nil)
			require.NoError(t, cmd.ParseFlags(append(tt.flags, "--project-id", "project-1")))

			err := cmd.Args(cmd, cmd.Flags().Args())

			requireUsefulError(t, err, usefulerror.ErrBadRequest, "Invalid ref", tt.wantHelp)
		})
	}
}

func TestRunCreate_ExplainsARefTheServerRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		refs     refFlags
		wantHelp string
	}{
		{
			name: "a branch suggests --tag",
			refs: refFlags{Branch: "v1.2.0", Set: []string{branchFlag}},
			wantHelp: "project project-2 cannot scan refs/heads/v1.2.0: git ref not found. " +
				"Check that refs/heads/v1.2.0 exists in every selected repository. If v1.2.0 is a tag, pass --tag v1.2.0.",
		},
		{
			name: "a tag does not suggest --tag",
			refs: refFlags{Tag: "v1.2.0", Set: []string{tagFlag}},
			wantHelp: "project project-2 cannot scan refs/tags/v1.2.0: git ref not found. " +
				"Check that refs/tags/v1.2.0 exists in every selected repository.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			serverMessage, _, _ := strings.Cut(tt.wantHelp, ". Check")
			scanner := &fakeCreateProjectScansClient{err: status.Error(codes.InvalidArgument, serverMessage)}

			_, err := runCreate(context.Background(), scanner, &fakeListProjectsClient{}, createInput{
				ProjectIDs: []string{"project-1", "project-2"},
				Refs:       tt.refs,
			})

			requireUsefulError(t, err, usefulerror.ErrBadRequest, "Ref cannot be scanned", tt.wantHelp)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
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
		Refs:       refFlags{Branch: "main", Set: []string{branchFlag}},
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

// FuzzRefFlags checks the rule that maps any value of each flag to the ref the
// API receives. --branch and --tag add their namespace and refuse a value that
// already has one. --ref sends a value under refs/ as given and treats any
// other value as a branch.
func FuzzRefFlags(f *testing.F) {
	for _, seed := range []string{
		"main", "release/1.2", "refs/heads/main", "refs/tags/v1.2.0", "refs/pull/1/head",
		"refs/", "refs", "heads/main", "tags/v1", " refs/heads/main", "REFS/heads/main",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if value == "" {
			return
		}
		underRefs := strings.HasPrefix(value, "refs/")

		ref, err := refFlags{Ref: value, Set: []string{refFlag}}.qualified()
		require.NoError(t, err)
		if underRefs {
			assert.Equal(t, value, ref)
		} else {
			assert.Equal(t, "refs/heads/"+value, ref)
		}

		for flag, prefix := range map[string]string{branchFlag: "refs/heads/", tagFlag: "refs/tags/"} {
			flags := refFlags{Branch: value, Tag: value, Set: []string{flag}}
			ref, err := flags.qualified()
			if underRefs {
				require.Error(t, err, flag)
				continue
			}
			require.NoError(t, err, flag)
			assert.Equal(t, prefix+value, ref, flag)
		}
	})
}
