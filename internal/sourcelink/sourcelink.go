// Package sourcelink picks the tenant's GitHub or Bitbucket link that a
// command works through, from the account name a person knows instead of the
// internal link ID. It lives outside internal/cmd because both the project
// and the integration command trees need it, and sibling command packages
// must not import each other.
package sourcelink

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/safedep/dry/usefulerror"
)

type Source string

const (
	GitHub    Source = "github"
	Bitbucket Source = "bitbucket"
)

// Link is one source link of the tenant. Account is the GitHub organization
// or user login, or the Bitbucket workspace slug. The server leaves it empty
// when it has no account name for the link.
type Link struct {
	Source  Source
	ID      string
	Account string
}

// Selection is what the caller asked for. An empty Source allows both
// sources.
type Selection struct {
	Source  Source
	Account string
	LinkID  string
}

// Group is the repository names one link serves, in argument order.
type Group struct {
	Link  Link
	Names []string
}

// Label names a link for people: its account, or its ID when the link has no
// account name.
func Label(account, linkID string) string {
	if account != "" {
		return account
	}
	return "link " + linkID
}

// Describe names a link in a sentence: "account safedep", or "link <id>"
// when the link has no account name.
func Describe(account, linkID string) string {
	if account != "" {
		return "account " + account
	}
	return "link " + linkID
}

// Pick returns the one link the selection names. A link ID wins, and an
// account given with it must be the link's account. An account matches
// without regard to case, as GitHub logins do. Without either, the tenant
// must have exactly one link of the allowed sources.
func Pick(links []Link, sel Selection) (Link, error) {
	links = ofSource(links, sel.Source)
	if sel.LinkID != "" {
		return pickByID(links, sel)
	}
	if len(links) == 0 {
		return Link{}, noLinkError(sel.Source)
	}
	if sel.Account != "" {
		matches := ofAccount(links, sel.Account)
		if len(matches) == 0 {
			return Link{}, notLinkedError(sel.Account, sel.Source, links)
		}
		return pickMatch(sel.Account, matches)
	}
	if len(links) > 1 {
		return Link{}, ambiguousError(links)
	}
	return links[0], nil
}

// GroupByOwner assigns every OWNER/REPOSITORY name to a link. An account or
// a link ID in the selection serves every name. Otherwise the owner of each
// name picks the link with that account. When no link has the owner's
// account and the allowed sources hold exactly one link, that link serves
// the name: the account name can be stale or missing, and a single link
// needs no choice.
func GroupByOwner(links []Link, sel Selection, names []string) ([]Group, error) {
	if sel.LinkID != "" || sel.Account != "" {
		link, err := Pick(links, sel)
		if err != nil {
			return nil, err
		}
		return []Group{{Link: link, Names: append([]string{}, names...)}}, nil
	}

	allowed := ofSource(links, sel.Source)
	var groups []Group
	index := map[string]int{}
	for _, name := range names {
		owner, _, _ := strings.Cut(name, "/")
		link, err := pickOwner(allowed, sel.Source, owner)
		if err != nil {
			return nil, err
		}
		i, ok := index[link.ID]
		if !ok {
			i = len(groups)
			index[link.ID] = i
			groups = append(groups, Group{Link: link})
		}
		groups[i].Names = append(groups[i].Names, name)
	}
	return groups, nil
}

func pickOwner(links []Link, source Source, owner string) (Link, error) {
	if len(links) == 0 {
		return Link{}, noLinkError(source)
	}
	matches := ofAccount(links, owner)
	if len(matches) > 0 {
		return pickMatch(owner, matches)
	}
	if len(links) == 1 {
		return links[0], nil
	}
	return Link{}, notLinkedError(owner, source, links)
}

func pickByID(links []Link, sel Selection) (Link, error) {
	for _, link := range links {
		if link.ID != sel.LinkID {
			continue
		}
		if sel.Account != "" && !strings.EqualFold(link.Account, sel.Account) {
			return Link{}, conflictError(link, sel.Account)
		}
		return link, nil
	}
	return Link{}, usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrNotFound).
		WithHumanError("Source link not found").
		WithHelp("Drop --link-id and retry with --account set to the GitHub organization or user, or the Bitbucket workspace.").
		Wrap(fmt.Errorf("link %q is not a %s link of the active tenant", sel.LinkID, sourceNoun(sel.Source)))
}

func pickMatch(account string, matches []Link) (Link, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	if spansSources(matches) {
		return Link{}, usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrBadRequest).
			WithHumanError(fmt.Sprintf("Account %q is linked on GitHub and Bitbucket", account)).
			WithHelp("Retry with --source github or --source bitbucket.").
			Wrap(fmt.Errorf("account %q has a GitHub link and a Bitbucket link", account))
	}
	// One account on two links of one source happens when GitHub misses an
	// uninstall webhook, or when a login is reused. GitHub links never get a
	// revocation time, so the server lists both. Only the link ID tells them
	// apart.
	ids := make([]string, 0, len(matches))
	for _, link := range matches {
		ids = append(ids, link.ID)
	}
	sort.Strings(ids)
	return Link{}, usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError(fmt.Sprintf("Account %q has more than one %s link", account, sourceTitle(matches[0].Source))).
		WithHelp(fmt.Sprintf("Retry with --link-id set to one of: %s.", strings.Join(ids, ", "))).
		Wrap(fmt.Errorf("account %q has %d %s links", account, len(matches), matches[0].Source))
}

func noLinkError(source Source) error {
	switch source {
	case GitHub:
		return usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrNotFound).
			WithHumanError("No GitHub App installation link").
			WithHelp("Install the SafeDep GitHub App and link it to this tenant, then retry.").
			Wrap(errors.New("the active tenant has no GitHub App installation link"))
	case Bitbucket:
		return usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrNotFound).
			WithHumanError("No linked Bitbucket workspace").
			WithHelp("Create a link code with `safedep integration bitbucket link create`, redeem it in the " +
				"workspace's SafeDep settings page in Bitbucket, then retry.").
			Wrap(errors.New("the active tenant has no linked bitbucket workspace"))
	default:
		return usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrNotFound).
			WithHumanError("No source integration link").
			WithHelp("Install and link the SafeDep GitHub App, or link a Bitbucket workspace, then retry.").
			Wrap(errors.New("the active tenant has no GitHub or Bitbucket link"))
	}
}

func notLinkedError(account string, source Source, links []Link) error {
	var addHelp string
	switch source {
	case GitHub:
		addHelp = fmt.Sprintf("To add %q, install the SafeDep GitHub App on it and link it in SafeDep Cloud.", account)
	case Bitbucket:
		addHelp = fmt.Sprintf("To add %q, create a link code with `safedep integration bitbucket link create` "+
			"and redeem it in the workspace's SafeDep settings page in Bitbucket.", account)
	default:
		addHelp = fmt.Sprintf("To add %q, link the GitHub organization or user in SafeDep Cloud, "+
			"or link the Bitbucket workspace with `safedep integration bitbucket link create`.", account)
	}
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrNotFound).
		WithHumanError(fmt.Sprintf("No linked account named %q", account)).
		WithHelp(fmt.Sprintf("Linked accounts: %s. %s", strings.Join(choices(links, false), ", "), addHelp)).
		Wrap(fmt.Errorf("no %s link of the active tenant has account %q", sourceNoun(source), account))
}

func ambiguousError(links []Link) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError("More than one linked account").
		WithHelp(fmt.Sprintf("Retry with one of: %s.", strings.Join(choices(links, true), ", "))).
		Wrap(fmt.Errorf("the active tenant has %d source links", len(links)))
}

func conflictError(link Link, account string) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError("--account and --link-id name different links").
		WithHelp(fmt.Sprintf("Drop --link-id and retry with --account %s.", account)).
		Wrap(fmt.Errorf("link %q belongs to account %q, not %q", link.ID, link.Account, account))
}

// choices names each link the way a person picks it: by account, or by
// --link-id when the link has no account name. The source follows each
// entry only when the links span both sources.
func choices(links []Link, withFlag bool) []string {
	tagSource := spansSources(links)
	out := make([]string, 0, len(links))
	for _, link := range links {
		var entry string
		switch {
		case link.Account == "":
			entry = "--link-id " + link.ID
		case withFlag:
			entry = "--account " + link.Account
		default:
			entry = link.Account
		}
		if tagSource {
			entry += fmt.Sprintf(" (%s)", link.Source)
		}
		out = append(out, entry)
	}
	sort.Strings(out)
	return out
}

func ofSource(links []Link, source Source) []Link {
	if source == "" {
		return links
	}
	var out []Link
	for _, link := range links {
		if link.Source == source {
			out = append(out, link)
		}
	}
	return out
}

func ofAccount(links []Link, account string) []Link {
	var out []Link
	for _, link := range links {
		if link.Account != "" && strings.EqualFold(link.Account, account) {
			out = append(out, link)
		}
	}
	return out
}

func spansSources(links []Link) bool {
	for _, link := range links {
		if link.Source != links[0].Source {
			return true
		}
	}
	return false
}

func sourceTitle(source Source) string {
	if source == Bitbucket {
		return "Bitbucket"
	}
	return "GitHub"
}

func sourceNoun(source Source) string {
	if source == "" {
		return "GitHub or Bitbucket"
	}
	return sourceTitle(source)
}
