package workload

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/humanize"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

const (
	grantPrefix          = "CREDENTIAL_GRANT_"
	operatorPrefix       = "CLAIM_CONDITION_OPERATOR_"
	projectBindingPrefix = "WORKLOAD_PROJECT_BINDING_"

	claimRepositoryID = "repository_id"
	claimRef          = "ref"
	claimEventName    = "event_name"
)

type repositoryView struct {
	ID       string `json:"repository_id"`
	FullName string `json:"full_name"`
}

type conditionView struct {
	Claim    string   `json:"claim"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

// text is the condition for a person, as in "ref = refs/heads/main" or
// "ref matches refs/heads/release/*". The operator enum is open, and the CLI
// does not guess the meaning of an operator that it does not know.
func (c conditionView) text() string {
	values := strings.Join(c.Values, ", ")
	switch c.Operator {
	case "equals":
		return c.Claim + " = " + values
	case "prefix":
		return c.Claim + " matches " + values
	}
	return c.Claim + ": a condition that this CLI cannot show"
}

type ruleView struct {
	ID       string `json:"rule_id"`
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	// ProviderID stays even when the provider name is known, because a
	// script matches the id.
	ProviderID   string           `json:"provider_id"`
	Grant        string           `json:"grant"`
	Repositories []repositoryView `json:"repositories"`
	// EveryRepository is true for a rule with no repository_id condition. It
	// covers every repository of the account of its provider, also those
	// that the GitHub App installation does not select.
	EveryRepository bool            `json:"every_repository"`
	Conditions      []conditionView `json:"conditions"`
	ProjectBinding  string          `json:"project_binding"`
	MaxLifetime     string          `json:"max_lifetime,omitempty"`
	CreatedAt       string          `json:"created_at,omitempty"`
	LastUsedAt      string          `json:"last_used_at,omitempty"`
	maxLifetime     time.Duration
	created         *time.Time
	lastUsed        *time.Time
}

// newRuleView builds the view of a rule. names maps provider ids to their
// KIND:ACCOUNT names, and may be nil.
func newRuleView(r *controltowerv1.WorkloadTrustRule, names map[string]string) ruleView {
	v := ruleView{
		ID:              r.GetRuleId(),
		Name:            r.GetName(),
		Provider:        names[r.GetProviderId()],
		ProviderID:      r.GetProviderId(),
		Grant:           grantToken(r.GetGrant()),
		Repositories:    make([]repositoryView, 0, len(r.GetRepositories())),
		EveryRepository: true,
		Conditions:      make([]conditionView, 0, len(r.GetConditions())),
		ProjectBinding:  tui.EnumToken(r.GetProjectBinding().String(), projectBindingPrefix),
		created:         cloudpolicy.TimeOf(r.GetCreatedAt()),
		lastUsed:        cloudpolicy.TimeOf(r.GetLastUsedAt()),
	}
	for _, repo := range r.GetRepositories() {
		v.Repositories = append(v.Repositories, repositoryView{ID: repo.GetRepositoryId(), FullName: repo.GetFullName()})
	}
	for _, c := range r.GetConditions() {
		if c.GetClaim() == claimRepositoryID {
			v.EveryRepository = false
		}
		v.Conditions = append(v.Conditions, conditionView{
			Claim:    c.GetClaim(),
			Operator: tui.EnumToken(c.GetOperator().String(), operatorPrefix),
			Values:   c.GetValues(),
		})
	}
	if r.HasMaxLifetime() {
		v.maxLifetime = r.GetMaxLifetime().AsDuration()
		v.MaxLifetime = v.maxLifetime.String()
	}
	if v.created != nil {
		v.CreatedAt = v.created.Format(time.RFC3339)
	}
	if v.lastUsed != nil {
		v.LastUsedAt = v.lastUsed.Format(time.RFC3339)
	}
	return v
}

func grantToken(g messagescontroltowerv1.CredentialGrant) string {
	return tui.EnumToken(g.String(), grantPrefix)
}

// provider is the provider for a person: the KIND:ACCOUNT name when the CLI
// knows it, else the id.
func (v ruleView) provider() string {
	if v.Provider != "" {
		return v.Provider
	}
	return v.ProviderID
}

// account is the account of the provider, as in acme, for the text about a
// rule that covers every repository.
func (v ruleView) account() string {
	if _, account, ok := strings.Cut(v.Provider, ":"); ok {
		return account
	}
	return "the provider account"
}

// repositoryNames lists the repositories of the rule, or says that the rule
// covers every repository of the account.
func (v ruleView) repositoryNames() []string {
	if v.EveryRepository {
		return []string{"every repository of " + v.account()}
	}
	names := make([]string, 0, len(v.Repositories))
	for _, r := range v.Repositories {
		names = append(names, r.FullName+" ("+r.ID+")")
	}
	return names
}

// repositorySummary is the repositories of the rule on one line, by full
// name, or the text for a rule that covers every repository.
func (v ruleView) repositorySummary() string {
	if v.EveryRepository {
		return "every repository of " + v.account()
	}
	names := make([]string, 0, len(v.Repositories))
	for _, r := range v.Repositories {
		names = append(names, r.FullName)
	}
	return strings.Join(names, ", ")
}

// repositoryPairs is the repositories of the rule as FULL_NAME=ID items, for
// the plain output.
func (v ruleView) repositoryPairs() string {
	pairs := make([]string, 0, len(v.Repositories))
	for _, r := range v.Repositories {
		pairs = append(pairs, r.FullName+"="+r.ID)
	}
	return strings.Join(pairs, ",")
}

// otherConditions are the conditions besides repository_id, which the
// repositories already show.
func (v ruleView) otherConditions() []string {
	var out []string
	for _, c := range v.Conditions {
		if c.Claim == claimRepositoryID {
			continue
		}
		out = append(out, c.text())
	}
	return out
}

func (v ruleView) lastUsedCell(now time.Time) string {
	if v.lastUsed == nil {
		return "never"
	}
	return humanize.Time(*v.lastUsed, now)
}

func (v ruleView) projectBindingText() string {
	switch v.ProjectBinding {
	case "auto":
		return "auto: the first tool session of a repository with no project creates its project"
	case "manual":
		return "manual: a repository with no project needs a project before a tool session"
	}
	return v.ProjectBinding
}

// ruleResult shows one rule. Rule create and rule show print it.
type ruleResult struct {
	Rule ruleView `json:"rule"`
}

func (r *ruleResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *ruleResult) RenderTable() string {
	v := r.Rule
	now := time.Now()
	rows := [][2]string{
		{"Rule", v.ID},
		{"Name", v.Name},
		{"Provider", v.provider()},
		{"Grant", v.Grant},
	}
	rows = appendRows(rows, "Repositories", v.repositoryNames())
	rows = appendRows(rows, "Conditions", v.otherConditions())
	created := "-"
	if v.created != nil {
		created = humanize.Time(*v.created, now)
	}
	maxLifetime := "-"
	if v.MaxLifetime != "" {
		maxLifetime = humanize.Duration(v.maxLifetime)
	}
	rows = append(rows,
		[2]string{"Projects", v.projectBindingText()},
		[2]string{"Max lifetime", maxLifetime},
		[2]string{"Created", created},
		[2]string{"Last used", v.lastUsedCell(now)},
	)
	return cloudpolicy.KeyValues(rows)
}

// appendRows writes one value on each row. A row after the first has an
// empty label, so it reads as part of the row above.
func appendRows(rows [][2]string, label string, values []string) [][2]string {
	if len(values) == 0 {
		return append(rows, [2]string{label, "-"})
	}
	for i, value := range values {
		if i > 0 {
			label = ""
		}
		rows = append(rows, [2]string{label, value})
	}
	return rows
}

func (r *ruleResult) RenderPlain() string {
	v := r.Rule
	return strings.Join([]string{
		tui.PlainRow("rule_id", v.ID),
		tui.PlainRow("name", v.Name),
		tui.PlainRow("provider", v.Provider),
		tui.PlainRow("provider_id", v.ProviderID),
		tui.PlainRow("grant", v.Grant),
		tui.PlainRow("repositories", v.repositoryPairs()),
		tui.PlainRow("every_repository", strconv.FormatBool(v.EveryRepository)),
		tui.PlainRow("conditions", strings.Join(v.otherConditions(), " and ")),
		tui.PlainRow("project_binding", v.ProjectBinding),
		tui.PlainRow("max_lifetime", v.MaxLifetime),
		tui.PlainRow("created_at", v.CreatedAt),
		tui.PlainRow("last_used_at", v.LastUsedAt),
	}, "\n")
}
