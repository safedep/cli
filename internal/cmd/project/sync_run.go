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
	// progress reports each --all batch on stderr. A nil progress reports
	// nothing.
	progress              func(format string, args ...any)
	githubLinks           githubLinkLister
	githubRepositories    githubRepositoryLister
	githubSyncer          githubProjectSyncer
	bitbucketLinks        bitbucketlink.Lister
	bitbucketRepositories bitbucketRepositoryLister
	bitbucketSyncer       bitbucketProjectSyncer
}

func (d syncDeps) report(format string, args ...any) {
	if d.progress != nil {
		d.progress(format, args...)
	}
}

// syncPlan is what one link syncs: the names it resolves and the immutable
// selectors the caller passed.
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
	var links []sourcelink.Link
	if _, direct := directLink(sel); !direct {
		listed, err := listSyncLinks(ctx, deps, sel.Source)
		if err != nil {
			return nil, err
		}
		links = listed
	}

	if in.All {
		link, err := pickLink(links, sel)
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
		return runBitbucketPlans(ctx, deps, plans)
	}
	return runGitHubPlans(ctx, deps, plans)
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

// directLink returns the link a hidden --link-id names when the source is
// known, so the run lists no links, as scripts that pass the ID expect.
func directLink(sel sourcelink.Selection) (sourcelink.Link, bool) {
	if sel.LinkID == "" || sel.Account != "" || sel.Source == "" {
		return sourcelink.Link{}, false
	}
	return sourcelink.Link{Source: sel.Source, ID: sel.LinkID}, true
}

func pickLink(links []sourcelink.Link, sel sourcelink.Selection) (sourcelink.Link, error) {
	if link, ok := directLink(sel); ok {
		return link, nil
	}
	return sourcelink.Pick(links, sel)
}

// listSyncLinks lists the links of the sources the selection allows. When
// both are allowed, a control plane without the Bitbucket RPCs counts as a
// tenant with no Bitbucket links, which Unimplemented proves. Without this,
// a GitHub-only tenant against such a control plane loses every names-only
// sync to the failing listing. Every other failure, PermissionDenied
// included, stops the sync: links the caller cannot inspect may exist, and
// picking GitHub then could sync a name against the wrong source.
func listSyncLinks(ctx context.Context, deps syncDeps, source sourcelink.Source) ([]sourcelink.Link, error) {
	var links []sourcelink.Link
	if source != sourcelink.Bitbucket {
		github, err := listGitHubLinks(ctx, deps.githubLinks)
		if err != nil {
			return nil, err
		}
		links = append(links, github...)
	}
	if source != sourcelink.GitHub {
		bitbucket, err := bitbucketlink.List(ctx, deps.bitbucketLinks, syncLinkLabel)
		if source == "" && status.Code(err) == codes.Unimplemented {
			bitbucket, err = nil, nil
		}
		if err != nil {
			return nil, err
		}
		links = append(links, bitbucketlink.ToSourceLinks(bitbucket)...)
	}
	return links, nil
}

// planSync gives each link its share of the selection. The immutable
// selectors go to the link the selection picks, and the names go to the
// link of their owner. One invocation serves one source, because the two
// sources identify repositories differently.
func planSync(links []sourcelink.Link, sel sourcelink.Selection, in syncInput) ([]syncPlan, error) {
	if link, ok := directLink(sel); ok {
		return []syncPlan{{
			link: link, names: in.RepositoryNames, ids: in.RepositoryIDs, uuids: in.RepositoryUUIDs,
		}}, nil
	}

	var plans []syncPlan
	if len(in.RepositoryIDs) > 0 || len(in.RepositoryUUIDs) > 0 {
		link, err := sourcelink.Pick(links, sel)
		if err != nil {
			return nil, err
		}
		plans = append(plans, syncPlan{link: link, ids: in.RepositoryIDs, uuids: in.RepositoryUUIDs})
	}
	if len(in.RepositoryNames) > 0 {
		groups, err := sourcelink.GroupByOwner(links, sel, in.RepositoryNames)
		if err != nil {
			return nil, err
		}
		plans = mergeGroups(plans, groups)
	}

	for _, plan := range plans[1:] {
		if plan.link.Source != plans[0].link.Source {
			cause := errors.New("project sync: the names resolve to GitHub and Bitbucket accounts")
			return nil, invalidRepositorySelectionError(cause,
				"The names resolve to GitHub and Bitbucket accounts. Sync each source in its own command.")
		}
	}
	return plans, nil
}

func mergeGroups(plans []syncPlan, groups []sourcelink.Group) []syncPlan {
	for _, group := range groups {
		if len(plans) > 0 && plans[0].link.ID == group.Link.ID {
			plans[0].names = group.Names
			continue
		}
		plans = append(plans, syncPlan{link: group.Link, names: group.Names})
	}
	return plans
}

// runGitHubPlans resolves the names of every plan before it sends the first
// sync request, so a bad name in any plan materializes no project.
func runGitHubPlans(ctx context.Context, deps syncDeps, plans []syncPlan) (*syncResult, error) {
	requests := make([]githubRequest, 0, len(plans))
	total := 0
	for _, plan := range plans {
		req, err := prepareGitHubRequest(ctx, deps.githubRepositories, plan)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
		total += len(req.ids)
	}

	result := &syncResult{}
	for _, req := range requests {
		projects, err := syncGitHubProjects(ctx, deps.githubSyncer, req)
		if err != nil {
			return nil, syncStoppedError(err, len(result.projects), total)
		}
		result.links = append(result.links, req.link)
		result.projects = append(result.projects, projects...)
	}
	return result, nil
}

func runBitbucketPlans(ctx context.Context, deps syncDeps, plans []syncPlan) (*bitbucketSyncResult, error) {
	requests := make([]bitbucketRequest, 0, len(plans))
	total := 0
	for _, plan := range plans {
		req, err := prepareBitbucketRequest(ctx, deps.bitbucketRepositories, plan)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
		total += len(req.uuids)
	}

	result := &bitbucketSyncResult{}
	for _, req := range requests {
		projects, err := syncBitbucketProjects(ctx, deps.bitbucketSyncer, req)
		if err != nil {
			return nil, syncStoppedError(err, len(result.projects), total)
		}
		result.links = append(result.links, req.link)
		result.projects = append(result.projects, projects...)
	}
	return result, nil
}
