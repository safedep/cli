package policy

import (
	"fmt"
	"strings"
	"time"

	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/humanize"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

// ruleView is the package rule that `rule list`, `rule show` and
// `rule create` print.

type ruleView struct {
	ID              string                    `json:"rule_id"`
	Purl            string                    `json:"purl"`
	Vers            string                    `json:"vers,omitempty"`
	Effect          string                    `json:"effect"`
	Scopes          []string                  `json:"scopes"`
	Vulnerabilities []waivedVulnerabilityView `json:"vulnerabilities,omitempty"`
	Reason          string                    `json:"reason"`
	Ticket          string                    `json:"ticket,omitempty"`
	RequestedBy     string                    `json:"requested_by,omitempty"`
	OverrideID      string                    `json:"override_id,omitempty"`
	AttachesTo      string                    `json:"attaches_to"`
	CreatedBy       string                    `json:"created_by,omitempty"`
	AddedRevision   uint64                    `json:"added_revision"`
	UpdatedRevision uint64                    `json:"updated_revision,omitempty"`
	RemovedRevision uint64                    `json:"removed_revision,omitempty"`
	ExpiresAt       string                    `json:"expires_at,omitempty"`
	CreatedAt       string                    `json:"created_at,omitempty"`
	UpdatedAt       string                    `json:"updated_at,omitempty"`
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
		Vulnerabilities: waivedViews(rule.GetVulnerabilities()),
		Reason:          rule.GetReason(),
		Ticket:          r.GetTicket(),
		RequestedBy:     r.GetRequestedBy(),
		OverrideID:      r.GetOverrideId(),
		AttachesTo:      baseAttachment,
		CreatedBy:       cloudpolicy.ActorName(r.GetCreatedBy()),
		AddedRevision:   r.GetAddedRevision(),
		UpdatedRevision: r.GetUpdatedRevision(),
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

// waivedVulnerabilityView is one vulnerability that a trust entry waives.
// The justification is its short OpenVEX name.
type waivedVulnerabilityView struct {
	ID            string `json:"id"`
	Justification string `json:"justification,omitempty"`
}

func waivedViews(waived []*corev1.WaivedVulnerability) []waivedVulnerabilityView {
	if len(waived) == 0 {
		return nil
	}
	out := make([]waivedVulnerabilityView, 0, len(waived))
	for _, w := range waived {
		out = append(out, waivedVulnerabilityView{ID: w.GetId(), Justification: justificationName(w.GetJustification())})
	}
	return out
}

// waivedText is the form for people, as
// "CVE-2024-3094 (vulnerable_code_not_in_execute_path), GHSA-jfh8-c2jp-5v3q".
func (v ruleView) waivedText() string {
	texts := make([]string, 0, len(v.Vulnerabilities))
	for _, w := range v.Vulnerabilities {
		text := w.ID
		if w.Justification != "" {
			text += " (" + w.Justification + ")"
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, ", ")
}

// waivedPlain is the form of the --vulnerability flag, as
// "CVE-2024-3094:vulnerable_code_not_in_execute_path,GHSA-jfh8-c2jp-5v3q".
func (v ruleView) waivedPlain() string {
	values := make([]string, 0, len(v.Vulnerabilities))
	for _, w := range v.Vulnerabilities {
		value := w.ID
		if w.Justification != "" {
			value += ":" + w.Justification
		}
		values = append(values, value)
	}
	return strings.Join(values, ",")
}

// scopeText is the scope cell of a table. A trust entry that names
// vulnerabilities shows them after its scope.
func (v ruleView) scopeText() string {
	text := strings.Join(v.Scopes, ",")
	if len(v.Vulnerabilities) > 0 {
		text += ": " + v.waivedText()
	}
	return cloudpolicy.EmptyDash(text)
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
	return expiryRelative(*v.expires, now)
}

// expiryText is a humanized expiry with its date, as "in 3d (2026-11-09)".
// Beyond 30 days humanize.Time gives the date itself, so the date shows one
// time only.
func expiryText(expires, now time.Time) string {
	rel := expiryRelative(expires, now)
	date := expires.UTC().Format(time.DateOnly)
	if rel == date {
		return rel
	}
	return rel + " (" + date + ")"
}

// expiryRelative is humanize.Time with a time of a day or more rounded to
// the nearest day. humanize.Time drops the part of a day, so a rule that
// --expires 7d sets reads "in 6d" a few seconds after its create.
func expiryRelative(expires, now time.Time) string {
	rel := humanize.Time(expires, now)
	d := expires.Sub(now)
	if d.Abs() < 24*time.Hour || rel == expires.UTC().Format(time.DateOnly) {
		return rel
	}
	if rounded := d.Round(24 * time.Hour); rounded.Abs() < 30*24*time.Hour {
		return humanize.Time(now.Add(rounded), now)
	}
	return rel
}

// blockPlace names where a block applies: the tenant for the policy base,
// or the subjects of the policy override that the rule attaches to.
func blockPlace(override string) string {
	if override == "" {
		return "in the tenant"
	}
	return fmt.Sprintf("on the subjects that policy override %q selects", tui.EscapeLine(override))
}

func scopeEffect(rule *corev1.PackageRule, scope corev1.TrustScope) string {
	switch scope {
	case corev1.TrustScope_TRUST_SCOPE_MALWARE:
		return "a malware verdict that is not verified"
	case corev1.TrustScope_TRUST_SCOPE_COOLDOWN:
		return "the dependency cooldown"
	case corev1.TrustScope_TRUST_SCOPE_VULNERABILITY:
		return "the vulnerability findings of a scan gate"
	case corev1.TrustScope_TRUST_SCOPE_LICENSE:
		return "the license findings of a scan gate"
	case corev1.TrustScope_TRUST_SCOPE_NAMED_VULNERABILITY:
		ids := make([]string, 0, len(rule.GetVulnerabilities()))
		for _, w := range rule.GetVulnerabilities() {
			ids = append(ids, w.GetId())
		}
		return "the scan gate findings of " + strings.Join(ids, ", ")
	default:
		return "the verdicts of scope " + scope.String()
	}
}
