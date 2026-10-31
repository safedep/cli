package policy

import (
	"fmt"
	"time"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/humanize"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

// ruleView is the package rule that `rule list`, `rule show` and
// `rule create` print.

type ruleView struct {
	ID              string   `json:"rule_id"`
	Purl            string   `json:"purl"`
	Vers            string   `json:"vers,omitempty"`
	Effect          string   `json:"effect"`
	Scopes          []string `json:"scopes"`
	Reason          string   `json:"reason"`
	Ticket          string   `json:"ticket,omitempty"`
	RequestedBy     string   `json:"requested_by,omitempty"`
	OverrideID      string   `json:"override_id,omitempty"`
	AttachesTo      string   `json:"attaches_to"`
	CreatedBy       string   `json:"created_by,omitempty"`
	AddedRevision   uint64   `json:"added_revision"`
	RemovedRevision uint64   `json:"removed_revision,omitempty"`
	ExpiresAt       string   `json:"expires_at,omitempty"`
	CreatedAt       string   `json:"created_at,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	expires         *time.Time
}

// newRuleView translates a rule. names maps override ids to names. A rule
// whose override is not in names shows the override id.
func newRuleView(r *policyv1.PackageRuleWithAttributes, names map[string]string) ruleView {
	rule := r.GetRule()
	expires := cloudpolicy.TimeOf(rule.GetExpiresAt())
	v := ruleView{
		ID:              rule.GetRuleId(),
		Purl:            rule.GetSelector().GetPurl(),
		Vers:            rule.GetSelector().GetVers(),
		Effect:          tui.EnumToken(rule.GetEffect().String(), effectPrefix),
		Scopes:          scopeTokens(rule.GetScopes()),
		Reason:          rule.GetReason(),
		Ticket:          r.GetTicket(),
		RequestedBy:     r.GetRequestedBy(),
		OverrideID:      r.GetOverrideId(),
		AttachesTo:      baseAttachment,
		CreatedBy:       cloudpolicy.ActorName(r.GetCreatedBy()),
		AddedRevision:   r.GetAddedRevision(),
		RemovedRevision: r.GetRemovedRevision(),
		ExpiresAt:       cloudpolicy.RFC3339(expires),
		CreatedAt:       cloudpolicy.RFC3339(cloudpolicy.TimeOf(r.GetCreatedAt())),
		UpdatedAt:       cloudpolicy.RFC3339(cloudpolicy.TimeOf(r.GetUpdatedAt())),
		expires:         expires,
	}
	if id := r.GetOverrideId(); id != "" {
		v.AttachesTo = "override " + id
		if name, ok := names[id]; ok {
			v.AttachesTo = fmt.Sprintf("override %q", name)
		}
	}
	return v
}

func (v ruleView) selector() string {
	if v.Vers == "" {
		return v.Purl
	}
	return v.Purl + " " + v.Vers
}

func (v ruleView) expiresCell(now time.Time) string {
	if v.expires == nil {
		return "never"
	}
	return humanize.Time(*v.expires, now)
}
