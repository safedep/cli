package packages

import (
	"context"
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

func TestResolveTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     string
		flags   targetFlags
		wantEco packagev1.Ecosystem
		wantN   string
		wantV   string
		wantErr string
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
			pv, err := resolveTarget(tt.ref, tt.flags)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantEco, pv.GetPackage().GetEcosystem())
			assert.Equal(t, tt.wantN, pv.GetPackage().GetName())
			assert.Equal(t, tt.wantV, pv.GetVersion())
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
