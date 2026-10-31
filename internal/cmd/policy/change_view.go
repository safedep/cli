package policy

import (
	"fmt"
	"strings"
	"time"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

type changeView struct {
	ID                string `json:"change_id"`
	Title             string `json:"title"`
	State             string `json:"state"`
	BaseRevision      uint64 `json:"base_revision"`
	PublishedRevision uint64 `json:"published_revision,omitempty"`
	Author            string `json:"author"`
	EditsSettings     bool   `json:"edits_settings"`
	RuleOperations    int    `json:"package_rule_operations"`
	RestoreFrom       uint64 `json:"restore_from_revision,omitempty"`
	Created           string `json:"created_at,omitempty"`
	Updated           string `json:"updated_at,omitempty"`
	updatedAt         *time.Time
}

func newChangeView(c *policyv1.PolicyChange) changeView {
	updated := cloudpolicy.TimeOf(c.GetUpdatedAt())
	v := changeView{
		ID:                c.GetChangeId(),
		Title:             c.GetTitle(),
		State:             tui.EnumToken(c.GetState().String(), changeStatePrefix),
		BaseRevision:      c.GetBaseRevision(),
		PublishedRevision: c.GetPublishedRevision(),
		Author:            cloudpolicy.ActorName(c.GetAuthor()),
		EditsSettings:     c.HasSettings(),
		RuleOperations:    len(c.GetPackageRuleOperations()),
		Created:           cloudpolicy.RFC3339(cloudpolicy.TimeOf(c.GetCreatedAt())),
		Updated:           cloudpolicy.RFC3339(updated),
		updatedAt:         updated,
	}
	if c.HasRestore() {
		v.RestoreFrom = c.GetRestore().GetFromRevision()
	}
	return v
}

func (v changeView) titleOrUntitled() string {
	if v.Title == "" {
		return "(untitled)"
	}
	return v.Title
}

// contents says what the change edits, as in "settings, 2 rule operations".
func (v changeView) contents() string {
	var parts []string
	if v.RestoreFrom > 0 {
		parts = append(parts, fmt.Sprintf("restore from revision %d", v.RestoreFrom))
	}
	if v.EditsSettings {
		parts = append(parts, "settings")
	}
	if v.RuleOperations > 0 {
		parts = append(parts, fmt.Sprintf("%d rule %s", v.RuleOperations, cloudpolicy.Plural(v.RuleOperations, "operation", "operations")))
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}
