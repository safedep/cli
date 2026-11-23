package project

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/bitbucketlink"
	"github.com/safedep/cli/internal/sourcelink"
	"github.com/safedep/cli/internal/tui"
)

const syncLinkLabel = "project sync: resolve workspace link"

type syncDeps struct {
	// progress reports each --all batch on stderr. Nil reports nothing.
	progress              func(format string, args ...any)
	githubLinks           githubLinkLister
	githubRepositories    githubRepositoryLister
	githubSyncer          githubProjectSyncer
	bitbucketLinks        bitbucketlink.Lister
	bitbucketRepositories bitbucketRepositoryLister
	bitbucketSyncer       bitbucketProjectSyncer
}

// syncPlan is what one link syncs.
type syncPlan struct {
	link  sourcelink.Link
	names []string
	ids   []int64
	uuids []string
}

func runSync(ctx context.Context, deps syncDeps, in syncInput) (tui.Renderable, error) {
	if err := normalizeSyncRepositoryUUIDs(in.RepositoryUUIDs); err != nil {
		return nil, err
	}
	if err := validateSyncInput(in); err != nil {
		return nil, err
	}

	sel := sourcelink.Selection{Source: selectedSource(in), Account: in.Account, LinkID: in.LinkID}
	links, err := syncLinks(ctx, deps, sel)
	if err != nil {
		return nil, err
	}

	if in.All {
		link, err := sourcelink.Pick(links, sel)
		if err != nil {
			return nil, err
		}
		return runSyncAll(ctx, deps, link)
	}

	plans, err := planSync(links, sel, in)
	if err != nil {
		return nil, err
	}
	if plans[0].link.Source == sourcelink.Bitbucket {
		projects, err := syncPlans(ctx, plans,
			func(ctx context.Context, plan syncPlan) (bitbucketRequest, error) {
				return prepareBitbucketRequest(ctx, deps.bitbucketRepositories, plan)
			},
			func(ctx context.Context, req bitbucketRequest) ([]syncedBitbucketProject, error) {
				return syncBitbucketProjects(ctx, deps.bitbucketSyncer, req)
			})
		if err != nil {
			return nil, err
		}
		return &bitbucketSyncResult{links: planLinks(plans), projects: projects}, nil
	}
	projects, err := syncPlans(ctx, plans,
		func(ctx context.Context, plan syncPlan) (githubRequest, error) {
			return prepareGitHubRequest(ctx, deps.githubRepositories, plan)
		},
		func(ctx context.Context, req githubRequest) ([]syncedProject, error) {
			return syncGitHubProjects(ctx, deps.githubSyncer, req)
		})
	if err != nil {
		return nil, err
	}
	return &syncResult{links: planLinks(plans), projects: projects}, nil
}

// selectedSource is the source the selection allows before any listing: an
// explicit --source, or the source an immutable selector belongs to. Empty
// allows both.
func selectedSource(in syncInput) sourcelink.Source {
	switch {
	case in.Source != "":
		return sourcelink.Source(in.Source)
	case len(in.RepositoryIDs) > 0:
		return sourcelink.GitHub
	case len(in.RepositoryUUIDs) > 0:
		return sourcelink.Bitbucket
	}
	return ""
}

// syncLinks lists the links of the sources the selection allows. A hidden
// --link-id with a known source lists nothing, as scripts expect. When both
// sources are allowed, Unimplemented from the Bitbucket listing means a
// control plane without those RPCs, so a GitHub-only tenant keeps working.
// Any other failure stops the sync, because links the caller cannot inspect
// may exist and a name could then sync against the wrong source.
func syncLinks(ctx context.Context, deps syncDeps, sel sourcelink.Selection) ([]sourcelink.Link, error) {
	if sel.LinkID != "" && sel.Account == "" && sel.Source != "" {
		return []sourcelink.Link{{Source: sel.Source, ID: sel.LinkID}}, nil
	}

	var links []sourcelink.Link
	if sel.Source != sourcelink.Bitbucket {
		github, err := listGitHubLinks(ctx, deps.githubLinks)
		if err != nil {
			return nil, err
		}
		links = append(links, github...)
	}
	if sel.Source != sourcelink.GitHub {
		bitbucket, err := bitbucketlink.List(ctx, deps.bitbucketLinks, syncLinkLabel)
		if sel.Source == "" && status.Code(err) == codes.Unimplemented {
			bitbucket, err = nil, nil
		}
		if err != nil {
			return nil, err
		}
		links = append(links, bitbucketlink.ToSourceLinks(bitbucket)...)
	}
	return links, nil
}

// planSync gives each link its share of the selection. Immutable selectors
// carry no owner, so with them the selection picks one link for everything.
// Otherwise the owner of each name picks its link. One invocation serves one
// source, because the two sources identify repositories differently.
func planSync(links []sourcelink.Link, sel sourcelink.Selection, in syncInput) ([]syncPlan, error) {
	if len(in.RepositoryIDs) > 0 || len(in.RepositoryUUIDs) > 0 {
		link, err := sourcelink.Pick(links, sel)
		if err != nil {
			return nil, err
		}
		return []syncPlan{{
			link: link, names: in.RepositoryNames, ids: in.RepositoryIDs, uuids: in.RepositoryUUIDs,
		}}, nil
	}

	groups, err := sourcelink.GroupByOwner(links, sel, in.RepositoryNames)
	if err != nil {
		return nil, err
	}
	plans := make([]syncPlan, 0, len(groups))
	for _, group := range groups {
		if group.Link.Source != groups[0].Link.Source {
			cause := errors.New("project sync: the names resolve to GitHub and Bitbucket accounts")
			return nil, invalidRepositorySelectionError(cause,
				"The names resolve to GitHub and Bitbucket accounts. Sync each source in its own command.")
		}
		plans = append(plans, syncPlan{link: group.Link, names: group.Names})
	}
	return plans, nil
}

// syncPlans prepares every plan before the first sync request, so a bad name
// in any plan materializes no project. It stops at the first failed request.
func syncPlans[R, P any](
	ctx context.Context,
	plans []syncPlan,
	prepare func(context.Context, syncPlan) (R, error),
	sync func(context.Context, R) ([]P, error),
) ([]P, error) {
	requests := make([]R, 0, len(plans))
	for _, plan := range plans {
		req, err := prepare(ctx, plan)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	var projects []P
	for i, req := range requests {
		synced, err := sync(ctx, req)
		if err != nil {
			return nil, linkStoppedError(err, planLinks(plans[:i]), plans[i].link)
		}
		projects = append(projects, synced...)
	}
	return projects, nil
}

func planLinks(plans []syncPlan) []sourcelink.Link {
	links := make([]sourcelink.Link, 0, len(plans))
	for _, plan := range plans {
		links = append(links, plan.link)
	}
	return links
}
