package policy

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	policyv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/policy/v1/policyv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/humanize"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

// The views below translate the proto messages that more than one verb
// prints. Renderers read the views, never the proto messages.

const (
	sep = cloudpolicy.Separator

	directionPrefix   = "CHANGE_DIRECTION_"
	sourcePrefix      = "CHANGE_SOURCE_"
	changeStatePrefix = "POLICY_CHANGE_STATE_"
	effectPrefix      = "EFFECT_"
	trustScopePrefix  = "TRUST_SCOPE_"
)

type revisionView struct {
	Revision    uint64     `json:"revision"`
	ChangeID    string     `json:"change_id,omitempty"`
	Author      string     `json:"author"`
	Message     string     `json:"message"`
	Source      string     `json:"source"`
	Direction   string     `json:"direction"`
	PublishedAt *time.Time `json:"-"`
	Published   string     `json:"published_at,omitempty"`
}

func newRevisionView(r *policyv1.PolicyRevision) *revisionView {
	if r == nil {
		return nil
	}
	published := timeOf(r.GetPublishedAt())
	return &revisionView{
		Revision:    r.GetRevision(),
		ChangeID:    r.GetChangeId(),
		Author:      actorName(r.GetAuthor()),
		Message:     r.GetMessage(),
		Source:      strings.ToUpper(tui.EnumToken(r.GetSource().String(), sourcePrefix)),
		Direction:   directionToken(r.GetDirection()),
		PublishedAt: published,
		Published:   cloudpolicy.RFC3339(published),
	}
}

// summary is the one-line form: the revision, when and by whom it was
// published, and the publish message.
func (r *revisionView) summary(now time.Time) string {
	parts := []string{fmt.Sprintf("%d", r.Revision)}
	published := "published"
	if r.PublishedAt != nil {
		published += " " + humanize.Time(*r.PublishedAt, now)
	}
	if r.Author != "" {
		published += " by " + r.Author
	}
	parts = append(parts, published)
	if r.Message != "" {
		parts = append(parts, fmt.Sprintf("%q", r.Message))
	}
	return strings.Join(parts, sep)
}

func actorName(a *messagescontroltowerv1.Actor) string {
	if a == nil {
		return ""
	}
	if a.GetDisplayName() != "" {
		return a.GetDisplayName()
	}
	return a.GetId()
}

func directionToken(d policyv1.ChangeDirection) string {
	return strings.ToUpper(tui.EnumToken(d.String(), directionPrefix))
}

func timeOf(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime().UTC()
	return &t
}

func humanTimeCell(t *time.Time, now time.Time) string {
	if t == nil {
		return "-"
	}
	return humanize.Time(*t, now)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

// overrideView is a policy override without its values.
type overrideView struct {
	ID          string `json:"override_id"`
	Name        string `json:"name"`
	Priority    uint32 `json:"priority"`
	Selector    string `json:"selector"`
	Description string `json:"description,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	expires     *time.Time
}

// overrideViews lists the policy overrides from the highest priority to the
// lowest, the order in which they win.
func overrideViews(p *policyv1.Policy) []overrideView {
	out := make([]overrideView, 0, len(p.GetOverrides()))
	for _, o := range p.GetOverrides() {
		expires := timeOf(o.GetExpiresAt())
		out = append(out, overrideView{
			ID:          o.GetOverrideId(),
			Name:        o.GetName(),
			Priority:    o.GetPriority(),
			Selector:    cloudpolicy.FormatSelector(o.GetSelector()),
			Description: o.GetDescription(),
			ExpiresAt:   cloudpolicy.RFC3339(expires),
			expires:     expires,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

// getPolicy reads the head policy of the tenant.
func getPolicy(ctx context.Context, client policyv1grpc.PolicyServiceClient) (*policyv1.GetPolicyResponse, error) {
	res, err := client.GetPolicy(ctx, &policyv1.GetPolicyRequest{})
	if err != nil {
		return nil, fmt.Errorf("policy: get policy: %w", err)
	}
	return res, nil
}

// findOverride returns the policy override with the name.
func findOverride(p *policyv1.Policy, name string) (*policyv1.Override, error) {
	names := make([]string, 0, len(p.GetOverrides()))
	for _, o := range p.GetOverrides() {
		if o.GetName() == name {
			return o, nil
		}
		names = append(names, o.GetName())
	}
	known := "the policy has no overrides"
	if len(names) > 0 {
		slices.Sort(names)
		known = "known overrides: " + strings.Join(names, ", ")
	}
	return nil, invalidInputError(
		fmt.Errorf("no policy override is named %q: %s", name, known),
		"See the policy overrides: safedep policy show",
	)
}

// overrideNames maps override ids to names, for output that cites an id.
func overrideNames(p *policyv1.Policy) map[string]string {
	out := make(map[string]string, len(p.GetOverrides()))
	for _, o := range p.GetOverrides() {
		out[o.GetOverrideId()] = o.GetName()
	}
	return out
}

func scopeTokens(scopes []corev1.TrustScope) []string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, tui.EnumToken(s.String(), trustScopePrefix))
	}
	return out
}
