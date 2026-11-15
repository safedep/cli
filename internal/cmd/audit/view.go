package audit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	auditv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/audit/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

type eventView struct {
	ID         string         `json:"event_id"`
	Actor      string         `json:"actor"`
	ActorKind  string         `json:"actor_kind"`
	Action     string         `json:"action"`
	Kind       string         `json:"resource_kind"`
	ResourceID string         `json:"resource_id"`
	Result     string         `json:"result"`
	Details    map[string]any `json:"details,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	OccurredAt string         `json:"occurred_at"`
	occurred   *time.Time
}

type listResult struct {
	Events        []eventView `json:"events"`
	NextPageToken string      `json:"next_page_token,omitempty"`
}

func newEventView(e *auditv1.AuditEvent) eventView {
	v := eventView{
		ID:         e.GetEventId(),
		Actor:      cloudpolicy.ActorName(e.GetActor()),
		ActorKind:  tui.EnumToken(e.GetActor().GetKind().String(), "ACTOR_KIND_"),
		Action:     e.GetAction(),
		Kind:       e.GetResource().GetKind(),
		ResourceID: e.GetResource().GetId(),
		Result:     tui.EnumToken(e.GetResult().String(), resultPrefix),
		RequestID:  e.GetRequestId(),
	}
	if e.HasDetails() {
		v.Details = e.GetDetails().AsMap()
	}
	if ts := e.GetOccurredAt(); ts != nil {
		t := ts.AsTime().UTC()
		v.occurred = &t
		v.OccurredAt = t.Format(time.RFC3339)
	}
	return v
}

func (v eventView) resource() string {
	if v.ResourceID == "" {
		return v.Kind
	}
	return v.Kind + " " + v.ResourceID
}

// detail writes the details as sorted "key=value" items.
func (v eventView) detail() string {
	keys := make([]string, 0, len(v.Details))
	for k := range v.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		value := v.Details[k]
		text := fmt.Sprint(value)
		if _, scalar := value.(string); !scalar {
			if b, err := json.Marshal(value); err == nil {
				text = string(b)
			}
		}
		parts = append(parts, k+"="+text)
	}
	return strings.Join(parts, ", ")
}

func (r *listResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *listResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.Events))
	for _, e := range r.Events {
		when := "-"
		if e.occurred != nil {
			when = humanize.Time(*e.occurred, now)
		}
		detail := e.detail()
		if detail == "" {
			detail = "-"
		}
		rows = append(rows, []string{when, e.Actor, e.Action, e.resource(), e.Result, detail})
	}
	t := table.New().
		Title("Audit events").
		Headers("WHEN", "ACTOR", "ACTION", "RESOURCE", "RESULT", "DETAIL").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No audit events match. Try a wider --since window.")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), cloudpolicy.Plural(len(rows), "event", "events"))
		if r.NextPageToken != "" {
			footer += ". More available: --page-token " + r.NextPageToken
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

func (r *listResult) RenderPlain() string {
	var b strings.Builder
	b.WriteString("occurred_at\tactor\taction\tresource_kind\tresource_id\tresult\tdetails")
	for _, e := range r.Events {
		b.WriteString("\n" + tui.PlainRow(e.OccurredAt, e.Actor, e.Action, e.Kind, e.ResourceID, e.Result, e.detail()))
	}
	return b.String()
}
