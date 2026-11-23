package project

import (
	"errors"
	"fmt"
	"strings"

	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/bitbucketlink"
	"github.com/safedep/cli/internal/sourcelink"
)

const (
	// SyncGitHubInstallationProjects accepts between one and one hundred
	// repository selections per request.
	maxSyncRepositories = 100

	// unknownRepositoryName marks a project materialized from a repository ID
	// the caller supplied directly, so the CLI never resolved its name.
	unknownRepositoryName = "-"
)

type syncInput struct {
	Source          string
	Account         string
	LinkID          string
	All             bool
	RepositoryNames []string
	RepositoryIDs   []int64
	RepositoryUUIDs []string
}

func syncCmd(a *app.App) *cobra.Command {
	var in syncInput
	cmd := &cobra.Command{
		Use:   "sync [OWNER/REPOSITORY...]",
		Short: "Sync GitHub or Bitbucket repositories into SafeDep projects",
		Long: "Materialize one SafeDep project per repository reachable through a linked " +
			"source integration: a GitHub App installation or a Bitbucket workspace. " +
			"Repository names are resolved to the source's immutable identity before the " +
			"request, and the sync is idempotent: repeating it returns the same project " +
			"for the same repository. The owner of each OWNER/REPOSITORY name picks the " +
			"linked GitHub organization or user, or Bitbucket workspace, with that name. " +
			"--all syncs every repository of one account and creates a project for every " +
			"repository not yet synced.",
		Args: func(_ *cobra.Command, args []string) error {
			in.RepositoryNames = args
			return validateSyncInput(in)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			in.RepositoryNames = args
			integration := newIntegrationClient(client.Connection())
			deps := syncDeps{
				progress:              tui.Info,
				githubLinks:           integration,
				githubRepositories:    integration,
				githubSyncer:          integration,
				bitbucketLinks:        integration,
				bitbucketRepositories: integration,
				bitbucketSyncer:       integration,
			}
			result, err := runSync(cmd.Context(), deps, in)
			if err != nil {
				return err
			}
			return a.Output.Print(result)
		},
	}

	f := cmd.Flags()
	f.StringVar(&in.Source, "source", "",
		"repository source: github or bitbucket (inferred from the account of each name, or from --repository-id and --repository-uuid, when omitted)")
	f.StringVar(&in.Account, "account", "",
		"GitHub organization or user, or Bitbucket workspace, to sync through; needed with --all, and with "+
			"--repository-id or --repository-uuid when the tenant has more than one link of that source")
	f.BoolVar(&in.All, "all", false,
		"sync every repository the account can read and create a project for every repository not yet synced; "+
			"on Bitbucket, only repositories with scan state enabled")
	// --link-id is hidden: people name a link by its account, and the flag
	// stays for scripts that pass the internal link ID.
	f.StringVar(&in.LinkID, "link-id", "", "source link ID to sync through")
	f.Lookup("link-id").Hidden = true
	f.Int64SliceVar(&in.RepositoryIDs, "repository-id", nil,
		"GitHub repository ID to sync instead of a name; repeat for multiple repositories")
	f.StringSliceVar(&in.RepositoryUUIDs, "repository-uuid", nil,
		"Bitbucket repository UUID to sync instead of a name; repeat for multiple repositories")
	// pflag renders an empty slice default as "(default [])", which reads as a
	// value the flag accepts. An empty DefValue suppresses the default in help
	// without changing the flag's zero value.
	f.Lookup("repository-id").DefValue = ""
	f.Lookup("repository-uuid").DefValue = ""
	return cmd
}

func validateSyncInput(in syncInput) error {
	if in.Source != "" && in.Source != string(sourcelink.GitHub) && in.Source != string(sourcelink.Bitbucket) {
		cause := fmt.Errorf("unknown --source value %q: allowed values are github, bitbucket", in.Source)
		return invalidRepositorySelectionError(cause, "Retry --source with github or bitbucket.")
	}
	if len(in.RepositoryIDs) > 0 && len(in.RepositoryUUIDs) > 0 {
		cause := fmt.Errorf("--repository-id selects GitHub and --repository-uuid selects Bitbucket")
		return invalidRepositorySelectionError(cause, "One sync serves one source. Drop one of the flags and retry.")
	}
	if in.Source == string(sourcelink.GitHub) && len(in.RepositoryUUIDs) > 0 {
		cause := fmt.Errorf("--repository-uuid selects Bitbucket repositories")
		return invalidRepositorySelectionError(cause, "Drop --repository-uuid, or use --source bitbucket.")
	}
	if in.Source == string(sourcelink.Bitbucket) && len(in.RepositoryIDs) > 0 {
		cause := fmt.Errorf("--repository-id selects GitHub repositories")
		return invalidRepositorySelectionError(cause, "Drop --repository-id, or use --source github.")
	}

	if strings.Contains(in.Account, "/") {
		cause := fmt.Errorf("--account %q is not an account name", in.Account)
		return invalidRepositorySelectionError(cause, "Pass the account name only, for example --account safedep.")
	}

	total := len(in.RepositoryIDs) + len(in.RepositoryNames) + len(in.RepositoryUUIDs)
	if in.All {
		return validateSyncAll(in, total)
	}
	if total < 1 || total > maxSyncRepositories {
		cause := fmt.Errorf("project sync requires between 1 and %d repositories", maxSyncRepositories)
		return invalidRepositorySelectionError(
			cause,
			fmt.Sprintf("Provide between 1 and %d repository names, --repository-id, or --repository-uuid values.", maxSyncRepositories),
		)
	}
	if err := validateRepositoryNames(in.RepositoryNames); err != nil {
		return err
	}
	if err := validateUniqueValues(in.RepositoryUUIDs, "repository UUID"); err != nil {
		return err
	}
	return validateRepositoryIDs(in.RepositoryIDs)
}

// normalizeSyncRepositoryUUIDs rewrites every --repository-uuid value to the
// canonical lowercase, unbraced form the control plane requires, so a UUID
// pasted from the Bitbucket UI works and a malformed value fails before any
// RPC. It runs before validation, so two spellings of one UUID fail the
// duplicate check.
func normalizeSyncRepositoryUUIDs(values []string) error {
	for i, value := range values {
		normalized, err := bitbucketlink.NormalizeRepositoryUUID(value)
		if err != nil {
			return invalidRepositorySelectionError(err, fmt.Sprintf(
				"Provide --repository-uuid value %d as a repository UUID from `safedep integration bitbucket repository list`. Braces and uppercase are accepted.",
				i+1))
		}
		values[i] = normalized
	}
	return nil
}

// validateRepositoryNames enforces the owner/repository shape and rejects
// duplicates case-insensitively, because GitHub treats owner and repository
// names as case-insensitive.
func validateRepositoryNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for i, name := range names {
		owner, repo, found := strings.Cut(name, "/")
		if !found || owner == "" || repo == "" || strings.Contains(repo, "/") {
			cause := fmt.Errorf("repository at position %d must use the OWNER/REPOSITORY form, got %q", i+1, name)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Provide repository %d as OWNER/REPOSITORY, for example safedep/cli.", i+1),
			)
		}

		key := repositoryKey(name)
		if _, duplicate := seen[key]; duplicate {
			cause := fmt.Errorf("duplicate repository %q", name)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Remove duplicate repository %q and retry.", name),
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateSyncAll(in syncInput, selected int) error {
	if in.Account == "" && in.LinkID == "" {
		cause := errors.New("--all needs --account")
		return invalidRepositorySelectionError(cause, "Pass --account with --all, for example --account safedep.")
	}
	if selected > 0 {
		cause := errors.New("--all takes no repository names, --repository-id or --repository-uuid")
		return invalidRepositorySelectionError(cause, "Drop the repository names and the ID flags, or drop --all.")
	}
	return nil
}

func validateRepositoryIDs(ids []int64) error {
	seen := make(map[int64]struct{}, len(ids))
	for i, id := range ids {
		if id <= 0 {
			cause := fmt.Errorf("repository ID at position %d must be greater than zero, got %d", i+1, id)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Provide a positive GitHub repository ID at position %d.", i+1),
			)
		}
		if _, duplicate := seen[id]; duplicate {
			cause := fmt.Errorf("duplicate repository ID %d", id)
			return invalidRepositorySelectionError(
				cause,
				fmt.Sprintf("Remove duplicate repository ID %d and retry.", id),
			)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func repositoryKey(name string) string {
	return strings.ToLower(name)
}
