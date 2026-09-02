package packages

import (
	"context"
	"errors"
	"testing"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeService is a configurable stub of the four scan interfaces. Unset
// funcs panic if called, so each test wires only what it exercises.
type fakeService struct {
	submitFn func(context.Context, SubmitInput) (*SubmitResult, error)
	getFn    func(context.Context, string) (*Scan, error)
	listFn   func(context.Context, ListInput) (*ListResult, error)
	reportFn func(context.Context, string) (*Report, error)

	gotSubmit SubmitInput
	gotList   ListInput
	getCalls  int
}

func (f *fakeService) Submit(ctx context.Context, in SubmitInput) (*SubmitResult, error) {
	f.gotSubmit = in
	return f.submitFn(ctx, in)
}

func (f *fakeService) Get(ctx context.Context, id string) (*Scan, error) {
	f.getCalls++
	return f.getFn(ctx, id)
}

func (f *fakeService) List(ctx context.Context, in ListInput) (*ListResult, error) {
	f.gotList = in
	return f.listFn(ctx, in)
}

func (f *fakeService) GetReport(ctx context.Context, id string) (*Report, error) {
	return f.reportFn(ctx, id)
}

func TestRegister_buildsPackageScanTree(t *testing.T) {
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)

	root := &cobra.Command{Use: "safedep"}
	Register(root, a)

	for _, path := range [][]string{
		{"package"},
		{"package", "scan"},
		{"package", "scan", "run"},
		{"package", "scan", "get"},
		{"package", "scan", "list"},
		{"package", "scan", "show"},
	} {
		cmd, _, err := root.Find(path)
		require.NoError(t, err, path)
		require.NotNil(t, cmd, path)
		assert.NotEmpty(t, cmd.Short, path)
		assert.NotEmpty(t, cmd.Long, path)
	}

	run, _, _ := root.Find([]string{"package", "scan", "run"})
	assert.NotNil(t, run.Flags().Lookup("ecosystem"))
	assert.NotNil(t, run.Flags().Lookup("wait"))
	assert.NotNil(t, run.Flags().Lookup("rescan"))

	for _, v := range []string{"get", "show"} {
		leaf, _, _ := root.Find([]string{"package", "scan", v})
		assert.NotNil(t, leaf.Flags().Lookup("scan-id"), v)
	}
}

// fakeResolver stands in for the GitHub client. A zero value fails the test
// when called, so non-GitHub paths prove they never touch the network.
type fakeResolver struct {
	t   *testing.T
	sha string
	err error

	calls    int
	gotOwner string
	gotRepo  string
	gotRef   string
}

func noResolver(t *testing.T) *fakeResolver {
	return &fakeResolver{t: t}
}

func (f *fakeResolver) ResolveCommitSHA(_ context.Context, owner, repo, ref string) (string, error) {
	f.calls++
	f.gotOwner, f.gotRepo, f.gotRef = owner, repo, ref
	if f.sha == "" && f.err == nil {
		f.t.Fatalf("unexpected ResolveCommitSHA(%q, %q, %q)", owner, repo, ref)
	}
	return f.sha, f.err
}

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func TestResolveTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		ref       string
		flags     targetFlags
		resolver  *fakeResolver
		wantEco   packagev1.Ecosystem
		wantN     string
		wantV     string
		wantErr   string
		wantCalls int
		wantOwner string
		wantRepo  string
		wantRef   string
	}{
		{
			name:    "explicit triple",
			flags:   targetFlags{Ecosystem: "npm", Name: "lodash", Version: "4.17.21"},
			wantEco: packagev1.Ecosystem_ECOSYSTEM_NPM,
			wantN:   "lodash", wantV: "4.17.21",
		},
		{
			name:    "purl npm scoped",
			ref:     "pkg:npm/@angular/core@12.0.0",
			wantEco: packagev1.Ecosystem_ECOSYSTEM_NPM,
			wantN:   "@angular/core", wantV: "12.0.0",
		},
		{
			name:    "purl vscode custom type",
			ref:     "pkg:vscode/publisher.ext@1.2.3",
			wantEco: packagev1.Ecosystem_ECOSYSTEM_VSCODE,
			wantN:   "publisher.ext", wantV: "1.2.3",
		},
		{
			name:     "purl github branch pinned to commit",
			ref:      "pkg:github/safedep/vet@main",
			resolver: &fakeResolver{sha: testSHA},
			wantEco:  packagev1.Ecosystem_ECOSYSTEM_GITHUB_ACTIONS,
			wantN:    "safedep/vet", wantV: testSHA,
			wantCalls: 1, wantOwner: "safedep", wantRepo: "vet", wantRef: "main",
		},
		{
			name:     "purl github actions subpath uses the repository",
			ref:      "pkg:github/aws-actions/configure-aws-credentials/assume-role@v2",
			resolver: &fakeResolver{sha: testSHA},
			wantEco:  packagev1.Ecosystem_ECOSYSTEM_GITHUB_ACTIONS,
			wantN:    "aws-actions/configure-aws-credentials/assume-role", wantV: testSHA,
			wantCalls: 1, wantOwner: "aws-actions", wantRepo: "configure-aws-credentials", wantRef: "v2",
		},
		{
			name:    "purl github commit passes through without a lookup",
			ref:     "pkg:github/safedep/vet@" + testSHA,
			wantEco: packagev1.Ecosystem_ECOSYSTEM_GITHUB_ACTIONS,
			wantN:   "safedep/vet", wantV: testSHA,
		},
		{
			name:     "github url with tree ref pinned to commit",
			ref:      "https://github.com/safedep/vet/tree/release/1.0",
			resolver: &fakeResolver{sha: testSHA},
			wantEco:  packagev1.Ecosystem_ECOSYSTEM_GITHUB_REPOSITORY,
			wantN:    "safedep/vet", wantV: testSHA,
			wantCalls: 1, wantOwner: "safedep", wantRepo: "vet", wantRef: "release/1.0",
		},
		{
			name:     "github url without ref pins the default branch",
			ref:      "https://github.com/safedep/vet",
			resolver: &fakeResolver{sha: testSHA},
			wantEco:  packagev1.Ecosystem_ECOSYSTEM_GITHUB_REPOSITORY,
			wantN:    "safedep/vet", wantV: testSHA,
			wantCalls: 1, wantOwner: "safedep", wantRepo: "vet", wantRef: "",
		},
		{
			name:     "explicit github triple pinned to commit",
			flags:    targetFlags{Ecosystem: "github_repository", Name: "safedep/vet", Version: "v1.2.3"},
			resolver: &fakeResolver{sha: testSHA},
			wantEco:  packagev1.Ecosystem_ECOSYSTEM_GITHUB_REPOSITORY,
			wantN:    "safedep/vet", wantV: testSHA,
			wantCalls: 1, wantOwner: "safedep", wantRepo: "vet", wantRef: "v1.2.3",
		},
		{
			name:      "github lookup failure is actionable",
			ref:       "pkg:github/safedep/vet@main",
			resolver:  &fakeResolver{err: errors.New("404 Not Found")},
			wantErr:   `resolve GitHub ref "main" of safedep/vet: 404 Not Found: pass a commit SHA`,
			wantCalls: 1,
		},
		{
			name:    "github name without repository rejected",
			flags:   targetFlags{Ecosystem: "github_actions", Name: "safedep", Version: "main"},
			wantErr: "expected owner/repo",
		},
		{
			name:    "explicit missing version",
			flags:   targetFlags{Ecosystem: "npm", Name: "lodash"},
			wantErr: "together",
		},
		{
			name:    "explicit unknown ecosystem",
			flags:   targetFlags{Ecosystem: "bogus", Name: "x", Version: "1"},
			wantErr: "unknown ecosystem",
		},
		{
			name:    "purl unknown type rejected",
			ref:     "pkg:bogus/x@1.0.0",
			wantErr: "unknown ecosystem",
		},
		{
			name:    "purl without version rejected",
			ref:     "pkg:npm/lodash",
			wantErr: "missing version",
		},
		{
			name:    "no input",
			wantErr: "no package specified",
		},
		{
			name:    "unrecognised ref",
			ref:     "just-a-name",
			wantErr: "unrecognised package reference",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolver := tt.resolver
			if resolver == nil {
				resolver = noResolver(t)
			}
			resolver.t = t

			pv, err := resolveTarget(context.Background(), resolver, tt.ref, tt.flags)
			assert.Equal(t, tt.wantCalls, resolver.calls, "resolver calls")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantEco, pv.GetPackage().GetEcosystem())
			assert.Equal(t, tt.wantN, pv.GetPackage().GetName())
			assert.Equal(t, tt.wantV, pv.GetVersion())
			if tt.wantCalls > 0 {
				assert.Equal(t, tt.wantOwner, resolver.gotOwner)
				assert.Equal(t, tt.wantRepo, resolver.gotRepo)
				assert.Equal(t, tt.wantRef, resolver.gotRef)
			}
		})
	}
}

func TestIdempotencyKey_stableAndTargetScoped(t *testing.T) {
	t.Parallel()
	a, err := resolveExplicit(targetFlags{Ecosystem: "npm", Name: "lodash", Version: "4.17.21"})
	require.NoError(t, err)
	b, err := resolveExplicit(targetFlags{Ecosystem: "npm", Name: "lodash", Version: "4.17.21"})
	require.NoError(t, err)
	c, err := resolveExplicit(targetFlags{Ecosystem: "npm", Name: "lodash", Version: "5.0.0"})
	require.NoError(t, err)

	assert.Equal(t, idempotencyKey(a), idempotencyKey(b), "same target -> same key")
	assert.NotEqual(t, idempotencyKey(a), idempotencyKey(c), "different version -> different key")
}
