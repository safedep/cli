package workload

import (
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/tui/humanize"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

type exchangeView struct {
	ID              string            `json:"exchange_id"`
	Outcome         string            `json:"outcome"`
	DenialReason    string            `json:"denial_reason,omitempty"`
	RuleID          string            `json:"rule_id,omitempty"`
	ProviderID      string            `json:"provider_id,omitempty"`
	Grant           string            `json:"grant"`
	DisplayIdentity string            `json:"display_identity"`
	Claims          map[string]string `json:"claims,omitempty"`
	EndpointID      string            `json:"endpoint_id,omitempty"`
	CreatedAt       string            `json:"created_at,omitempty"`
	ExpiresAt       string            `json:"expires_at,omitempty"`
	RevokedAt       string            `json:"revoked_at,omitempty"`
	created         *time.Time
	expires         *time.Time
	revoked         *time.Time
}

func newExchangeView(e *controltowerv1.WorkloadExchange) exchangeView {
	v := exchangeView{
		ID:              e.GetExchangeId(),
		Outcome:         tui.EnumToken(e.GetOutcome().String(), outcomePrefix),
		DenialReason:    e.GetDenialReason(),
		RuleID:          e.GetRuleId(),
		ProviderID:      e.GetProviderId(),
		Grant:           grantToken(e.GetGrant()),
		DisplayIdentity: e.GetClaimSummary().GetDisplayIdentity(),
		EndpointID:      e.GetEndpointId(),
		created:         cloudpolicy.TimeOf(e.GetCreatedAt()),
		expires:         cloudpolicy.TimeOf(e.GetExpiresAt()),
		revoked:         cloudpolicy.TimeOf(e.GetRevokedAt()),
	}
	if claims := e.GetClaimSummary().GetClaims(); len(claims) > 0 {
		v.Claims = make(map[string]string, len(claims))
		for _, c := range claims {
			v.Claims[c.GetName()] = c.GetValue()
		}
	}
	v.CreatedAt = rfc3339(v.created)
	v.ExpiresAt = rfc3339(v.expires)
	v.RevokedAt = rfc3339(v.revoked)
	return v
}

func rfc3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// detail is what a person needs from one exchange: why the server denied
// it, or the state of the credential that it issued.
func (v exchangeView) detail(now time.Time) string {
	switch {
	case v.DenialReason != "":
		return v.DenialReason
	case v.revoked != nil:
		return "revoked " + humanize.Time(*v.revoked, now)
	case v.expires != nil && v.expires.After(now):
		return "expires " + humanize.Time(*v.expires, now)
	case v.expires != nil:
		return "expired " + humanize.Time(*v.expires, now)
	}
	return "-"
}
