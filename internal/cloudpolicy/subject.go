package cloudpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/tui"
)

// SubjectPolicyClient reads the policy of one subject.
type SubjectPolicyClient interface {
	GetSubjectPolicy(
		context.Context,
		*policyv1.GetSubjectPolicyRequest,
		...grpc.CallOption,
	) (*policyv1.GetSubjectPolicyResponse, error)
}

// NewSubject builds a subject of the policy.
func NewSubject(kind specv1.SubjectKind, id string) *specv1.Subject {
	s := &specv1.Subject{}
	s.SetKind(kind)
	s.SetId(id)
	return s
}

// SubjectPolicy is the intended policy of one subject, the source of each
// value, and the policy that each tool or check applied.
type SubjectPolicy struct {
	Kind            string
	Name            string
	ID              string
	Revision        uint64
	DefaultRevision uint64
	Matched         []MatchedOverride
	Labels          []string
	TrustRules      int
	BlockRules      int
	Fields          []SubjectField
	Applied         []AppliedPolicy
	Filtered        bool
}

// MatchedOverride is a policy override that matches the subject.
type MatchedOverride struct {
	ID       string `json:"override_id"`
	Name     string `json:"name"`
	Priority uint32 `json:"priority"`
}

// SubjectField is one field of the intended policy.
type SubjectField struct {
	Path       string
	Value      string
	Human      string
	Source     string
	Exceptions []string
}

// AppliedPolicy is the last policy that one tool or check reported.
type AppliedPolicy struct {
	Implementer     string
	Version         string
	Revision        uint64
	DefaultRevision uint64
	State           string
	RejectedReason  string
	FetchedAt       *time.Time
	AppliedAt       *time.Time
	ReportedAt      *time.Time
	Exceptions      []FieldException
}

// FieldException is one field that a tool did not apply as the policy says.
type FieldException struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

const (
	layerPrefix           = "LAYER_"
	applyStatePrefix      = "APPLY_STATE_"
	exceptionReasonPrefix = "FIELD_EXCEPTION_REASON_"
)

// GetSubjectPolicy reads the policy of a subject. name labels the subject in
// the output. fields limits the output to these contract paths and the paths
// below them.
func GetSubjectPolicy(
	ctx context.Context,
	client SubjectPolicyClient,
	subject *specv1.Subject,
	name string,
	fields []string,
) (*SubjectPolicy, error) {
	if err := ValidateFieldPaths(fields); err != nil {
		return nil, err
	}
	req := &policyv1.GetSubjectPolicyRequest{}
	req.SetSubject(subject)
	res, err := client.GetSubjectPolicy(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get subject policy: %w", err)
	}
	return translateSubjectPolicy(res, subject, name, fields), nil
}

func translateSubjectPolicy(
	res *policyv1.GetSubjectPolicyResponse,
	subject *specv1.Subject,
	name string,
	fields []string,
) *SubjectPolicy {
	intended := res.GetIntended()
	header := intended.GetHeader()
	out := &SubjectPolicy{
		Kind:            subjectKindName(subject.GetKind()),
		Name:            name,
		ID:              subject.GetId(),
		Revision:        header.GetRevision(),
		DefaultRevision: header.GetDefaultPolicyRevision(),
		Labels:          FormatLabels(res.GetLabels()),
		Filtered:        len(fields) > 0,
	}

	overrides := make(map[string]MatchedOverride, len(header.GetMatchedOverrides()))
	for _, o := range header.GetMatchedOverrides() {
		m := MatchedOverride{ID: o.GetOverrideId(), Name: o.GetName(), Priority: o.GetPriority()}
		out.Matched = append(out.Matched, m)
		overrides[m.ID] = m
	}

	for _, rule := range intended.GetSpec().GetCore().GetPackageRules() {
		switch rule.GetEffect() {
		case corev1.Effect_EFFECT_TRUST:
			out.TrustRules++
		case corev1.Effect_EFFECT_BLOCK:
			out.BlockRules++
		}
	}

	sources := make(map[string][]string)
	for _, s := range intended.GetFieldSources() {
		sources[s.GetPath()] = appendUnique(sources[s.GetPath()], sourceLabel(s, overrides))
	}

	for _, a := range res.GetApplied() {
		out.Applied = append(out.Applied, translateApplied(a))
	}

	for _, f := range Fields(intended.GetSpec()) {
		if !MatchesFieldFilter([]string{f.Path}, fields) {
			continue
		}
		field := SubjectField{Path: f.Path, Value: f.Value, Human: f.Human, Source: "-"}
		if s := sources[f.Path]; len(s) > 0 {
			field.Source = strings.Join(s, ", ")
		}
		for _, a := range out.Applied {
			for _, e := range a.Exceptions {
				if e.Path == f.Path {
					field.Exceptions = append(field.Exceptions, a.Implementer+": "+exceptionText(e))
				}
			}
		}
		out.Fields = append(out.Fields, field)
	}
	return out
}

func translateApplied(a *policyv1.AppliedPolicy) AppliedPolicy {
	report := a.GetReport()
	out := AppliedPolicy{
		Implementer:     a.GetImplementer(),
		Version:         a.GetImplementerVersion(),
		Revision:        report.GetRevision(),
		DefaultRevision: report.GetDefaultPolicyRevision(),
		State:           tui.EnumToken(report.GetState().String(), applyStatePrefix),
		RejectedReason:  report.GetRejectedReason(),
		FetchedAt:       timePtr(report.GetFetchedAt()),
		AppliedAt:       timePtr(report.GetAppliedAt()),
		ReportedAt:      timePtr(a.GetReportedAt()),
	}
	for _, e := range report.GetExceptions() {
		out.Exceptions = append(out.Exceptions, FieldException{
			Path:   e.GetPath(),
			Reason: tui.EnumToken(e.GetReason().String(), exceptionReasonPrefix),
			Detail: e.GetDetail(),
		})
	}
	return out
}

func exceptionText(e FieldException) string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + " (" + e.Detail + ")"
}

func sourceLabel(s *specv1.FieldSource, overrides map[string]MatchedOverride) string {
	switch s.GetLayer() {
	case recordv1.Layer_LAYER_DEFAULT_POLICY:
		return "default policy"
	case recordv1.Layer_LAYER_BASE:
		return "base"
	case recordv1.Layer_LAYER_OVERRIDE:
		if o, ok := overrides[s.GetOverrideId()]; ok {
			return fmt.Sprintf("override %q (%d)", o.Name, o.Priority)
		}
		return "override " + s.GetOverrideId()
	default:
		return tui.EnumToken(s.GetLayer().String(), layerPrefix)
	}
}

func subjectKindName(kind specv1.SubjectKind) string {
	return tui.EnumToken(kind.String(), "SUBJECT_KIND_")
}

func appendUnique(values []string, v string) []string {
	for _, existing := range values {
		if existing == v {
			return values
		}
	}
	return append(values, v)
}

func timePtr(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime().UTC()
	return &t
}

// PolicyLine summarises the intended policy in one line: the revision and
// the matched policy overrides, or "no override matches" and "base only".
func (p *SubjectPolicy) PolicyLine() string {
	parts := []string{fmt.Sprintf("revision %d", p.Revision)}
	if len(p.Matched) == 0 {
		parts = append(parts, "no override matches", "base only")
	} else {
		names := make([]string, 0, len(p.Matched))
		for _, o := range p.Matched {
			names = append(names, fmt.Sprintf("%s (%d)", o.Name, o.Priority))
		}
		parts = append(parts, "overrides "+strings.Join(names, ", "))
	}
	return strings.Join(parts, Separator)
}

type subjectFieldJSON struct {
	Path       string   `json:"path"`
	Value      string   `json:"value"`
	Source     string   `json:"source"`
	Exceptions []string `json:"exceptions,omitempty"`
}

type appliedPolicyJSON struct {
	Implementer     string           `json:"implementer"`
	Version         string           `json:"implementer_version,omitempty"`
	Revision        uint64           `json:"revision"`
	DefaultRevision uint64           `json:"default_policy_revision"`
	State           string           `json:"state"`
	RejectedReason  string           `json:"rejected_reason,omitempty"`
	FetchedAt       string           `json:"fetched_at,omitempty"`
	AppliedAt       string           `json:"applied_at,omitempty"`
	ReportedAt      string           `json:"reported_at,omitempty"`
	Exceptions      []FieldException `json:"exceptions,omitempty"`
}

type subjectPolicyJSON struct {
	Kind            string              `json:"kind"`
	Name            string              `json:"name,omitempty"`
	ID              string              `json:"id"`
	Revision        uint64              `json:"revision"`
	DefaultRevision uint64              `json:"default_policy_revision"`
	Matched         []MatchedOverride   `json:"matched_overrides"`
	Labels          []string            `json:"labels"`
	TrustRules      int                 `json:"trust_rules"`
	BlockRules      int                 `json:"block_rules"`
	Fields          []subjectFieldJSON  `json:"fields"`
	Applied         []appliedPolicyJSON `json:"applied"`
}

func (p *SubjectPolicy) RenderJSON() ([]byte, error) {
	out := subjectPolicyJSON{
		Kind: p.Kind, Name: p.Name, ID: p.ID,
		Revision: p.Revision, DefaultRevision: p.DefaultRevision,
		Matched: p.Matched, Labels: p.Labels,
		TrustRules: p.TrustRules, BlockRules: p.BlockRules,
		Fields:  make([]subjectFieldJSON, 0, len(p.Fields)),
		Applied: make([]appliedPolicyJSON, 0, len(p.Applied)),
	}
	if out.Matched == nil {
		out.Matched = []MatchedOverride{}
	}
	if out.Labels == nil {
		out.Labels = []string{}
	}
	for _, f := range p.Fields {
		out.Fields = append(out.Fields, subjectFieldJSON{
			Path: f.Path, Value: f.Value, Source: f.Source, Exceptions: f.Exceptions,
		})
	}
	for _, a := range p.Applied {
		out.Applied = append(out.Applied, appliedPolicyJSON{
			Implementer: a.Implementer, Version: a.Version,
			Revision: a.Revision, DefaultRevision: a.DefaultRevision,
			State: a.State, RejectedReason: a.RejectedReason,
			FetchedAt: RFC3339(a.FetchedAt), AppliedAt: RFC3339(a.AppliedAt), ReportedAt: RFC3339(a.ReportedAt),
			Exceptions: a.Exceptions,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func (p *SubjectPolicy) RenderTable() string {
	now := time.Now()
	title := strings.ToUpper(p.Kind[:1]) + p.Kind[1:]
	subject := p.Name
	if subject == "" {
		subject = p.ID
	} else if p.ID != "" && p.ID != p.Name {
		subject += " (" + p.ID + ")"
	}
	rows := [][2]string{
		{title, subject},
		{"Policy", p.PolicyLine()},
		{"Default policy", fmt.Sprintf("revision %d", p.DefaultRevision)},
		{"Package rules", fmt.Sprintf("%d (%d trust, %d block)", p.TrustRules+p.BlockRules, p.TrustRules, p.BlockRules)},
	}
	if !p.Filtered {
		rows = append(rows, [2]string{"Labels", emptyDash(strings.Join(p.Labels, "  "))})
	}

	fieldRows := make([][]string, 0, len(p.Fields))
	for _, f := range p.Fields {
		fieldRows = append(fieldRows, []string{f.Path, f.Human, f.Source, emptyDash(strings.Join(f.Exceptions, "; "))})
	}
	empty := "The policy sets no fields for this " + p.Kind + "."
	if p.Filtered {
		empty = "The policy sets none of the requested fields for this " + p.Kind + "."
	}
	fieldTable := table.New().
		Headers("FIELD", "VALUE", "SOURCE", "EXCEPTIONS").
		Rows(fieldRows...).
		EmptyMessage(empty).
		Render()

	appliedRows := make([][]string, 0, len(p.Applied))
	for _, a := range p.Applied {
		appliedRows = append(appliedRows, []string{
			a.Implementer, emptyDash(a.Version), fmt.Sprintf("%d", a.Revision), a.State,
			humanTime(a.FetchedAt, now), humanTime(a.ReportedAt, now),
		})
	}
	appliedTable := table.New().
		Title("Applied policy, as each tool reported it").
		Headers("TOOL", "VERSION", "REVISION", "STATE", "FETCHED", "REPORTED").
		Rows(appliedRows...).
		EmptyMessage("No tool or check reported the policy that it applies for this " + p.Kind + " yet.").
		Render()

	return KeyValues(rows) + "\n\n" + fieldTable + "\n\n" + appliedTable
}

func (p *SubjectPolicy) RenderPlain() string {
	var b strings.Builder
	b.WriteString("path\tvalue\tsource\texceptions")
	for _, f := range p.Fields {
		fmt.Fprintf(&b, "\n%s\t%s\t%s\t%s", f.Path, f.Value, f.Source, strings.Join(f.Exceptions, "; "))
	}
	for _, a := range p.Applied {
		fmt.Fprintf(&b, "\napplied:%s\t%d\t%s\t%s", a.Implementer, a.Revision, a.State, RFC3339(a.ReportedAt))
	}
	return b.String()
}

func humanTime(t *time.Time, now time.Time) string {
	if t == nil {
		return "-"
	}
	return humanize.Time(*t, now)
}

// RFC3339 writes an optional time in UTC, or "" when it is absent.
func RFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
