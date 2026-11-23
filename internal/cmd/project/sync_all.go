package project

import (
	"context"
	"fmt"
	"slices"

	"github.com/safedep/dry/usefulerror"

	"github.com/safedep/cli/internal/sourcelink"
	"github.com/safedep/cli/internal/tui"
)

// runSyncAll syncs every repository the link can read. It lists them all
// before the first write, so the batches cover one consistent listing.
func runSyncAll(ctx context.Context, deps syncDeps, link sourcelink.Link) (tui.Renderable, error) {
	if link.Source == sourcelink.Bitbucket {
		return runBitbucketSyncAll(ctx, deps, link)
	}
	return runGitHubSyncAll(ctx, deps, link)
}

func runGitHubSyncAll(ctx context.Context, deps syncDeps, link sourcelink.Link) (*syncResult, error) {
	repositories, err := listAllGitHubRepositories(ctx, deps.githubRepositories, link.ID)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(repositories))
	names := make(map[int64]string, len(repositories))
	for _, repository := range repositories {
		ids = append(ids, repository.id)
		names[repository.id] = repository.fullName
	}

	projects, err := syncInBatches(ctx, deps, ids, func(ctx context.Context, batch []int64) ([]syncedProject, error) {
		return syncGitHubProjects(ctx, deps.githubSyncer, githubRequest{link: link, ids: batch, names: names})
	})
	if err != nil {
		return nil, err
	}
	return &syncResult{links: []sourcelink.Link{link}, projects: projects, all: true}, nil
}

func runBitbucketSyncAll(ctx context.Context, deps syncDeps, link sourcelink.Link) (*bitbucketSyncResult, error) {
	repositories, notEnabled, err := listEnabledBitbucketRepositories(ctx, deps.bitbucketRepositories, link.ID)
	if err != nil {
		return nil, err
	}
	if len(repositories) == 0 && notEnabled > 0 {
		return nil, noEnabledRepositoryError(link, notEnabled)
	}

	uuids := make([]string, 0, len(repositories))
	names := make(map[string]string, len(repositories))
	for _, repository := range repositories {
		uuids = append(uuids, repository.uuid)
		names[repository.uuid] = repository.fullName
	}

	projects, err := syncInBatches(ctx, deps, uuids, func(ctx context.Context, batch []string) ([]syncedBitbucketProject, error) {
		return syncBitbucketProjects(ctx, deps.bitbucketSyncer, bitbucketRequest{link: link, uuids: batch, names: names})
	})
	if err != nil {
		return nil, err
	}
	return &bitbucketSyncResult{
		links: []sourcelink.Link{link}, projects: projects, all: true, notEnabled: notEnabled,
	}, nil
}

// syncInBatches sends the selections in requests of at most
// maxSyncRepositories, the server's cap, and stops at the first failure.
func syncInBatches[T, P any](
	ctx context.Context,
	deps syncDeps,
	items []T,
	sync func(context.Context, []T) ([]P, error),
) ([]P, error) {
	var synced []P
	for batch := range slices.Chunk(items, maxSyncRepositories) {
		deps.report("Syncing repositories %d to %d of %d", len(synced)+1, len(synced)+len(batch), len(items))
		projects, err := sync(ctx, batch)
		if err != nil {
			return nil, syncStoppedError(err, len(synced), len(items))
		}
		synced = append(synced, projects...)
	}
	return synced, nil
}

func noEnabledRepositoryError(link sourcelink.Link, notEnabled int) error {
	account := sourcelink.Describe(link.Account, link.ID)
	command := "safedep integration bitbucket allowlist update"
	if link.Account != "" {
		command += " --account " + link.Account
	}
	return newProjectError(
		usefulerror.ErrNotFound,
		fmt.Sprintf("No repository of %s has scan state enabled", account),
		fmt.Sprintf("SafeDep syncs only repositories it scans, and %d %s not enabled. "+
			"Enable them with `%s`, then retry.",
			notEnabled, pluralRepositoriesAre(notEnabled), command),
		fmt.Errorf("project sync: %s has %d repositories and none is enabled", account, notEnabled),
	)
}

func pluralRepositoriesAre(n int) string {
	if n == 1 {
		return "repository is"
	}
	return "repositories are"
}
