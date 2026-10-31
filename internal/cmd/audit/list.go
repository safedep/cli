package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	auditv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/audit/v1/auditv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	auditv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/audit/v1"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/safedep/dry/usefulerror"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/paging"
	"github.com/safedep/cli/internal/tui"
)

const resultPrefix = "AUDIT_RESULT_"

// eventLister is the one RPC that the command calls.
type eventLister interface {
	ListAuditEvents(
		context.Context,
		*auditv1.ListAuditEventsRequest,
		...grpc.CallOption,
	) (*auditv1.ListAuditEventsResponse, error)
}

type listInput struct {
	Since     time.Duration
	Until     time.Duration
	Action    string
	Resource  string
	Actor     string
	PageSize  uint32
	PageToken string
}

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

func eventListCmd(a *app.App) *cobra.Command {
	var in listInput
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit events, newest first",
		Long: "List the audit events of the tenant, newest first. --since and --until take a duration " +
			"before now, as in 7d or 12h. --action takes a prefix of the action, as in policy. or " +
			"policy.rule. --resource takes a resource kind, or a kind and an id, as in policy_change/18. " +
			"--actor matches the actor id or the display name exactly. One page is returned per " +
			"invocation, so follow --page-token to read the next page.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			_, err := newListRequest(in, time.Now())
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			res, err := runList(cmd.Context(), auditv1grpc.NewAuditServiceClient(client.Connection()), in, time.Now())
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.Var(duration.NewValue(&in.Since, 0), "since", "only events in this time before now, as in 7d or 12h")
	f.Var(duration.NewValue(&in.Until, 0), "until", "only events older than this time before now, as in 1d")
	f.StringVar(&in.Action, "action", "", "only events whose action starts with this text, as in policy.")
	f.StringVar(&in.Resource, "resource", "", "only events about a resource: KIND or KIND/ID, as in policy_change/18")
	f.StringVar(&in.Actor, "actor", "", "only events of this actor id or display name, as in alice@acme.io")
	f.Uint32Var(&in.PageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&in.PageToken, "page-token", "", "continuation token from a prior response")
	return cmd
}

func invalidFilter(cause error, help string) error {
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError(cause.Error()).
		WithHelp(help).
		Wrap(cause)
}

func newListRequest(in listInput, now time.Time) (*auditv1.ListAuditEventsRequest, error) {
	filters := &auditv1.ListAuditEventsRequest_Filters{}
	set := false
	if in.Since < 0 || in.Until < 0 {
		return nil, invalidFilter(errors.New("--since and --until must not be negative"), "Use a duration such as 7d")
	}
	if in.Since > 0 {
		filters.SetOccurredAfter(timestamppb.New(now.Add(-in.Since)))
		set = true
	}
	if in.Until > 0 {
		if in.Since > 0 && in.Until >= in.Since {
			return nil, invalidFilter(errors.New("--until must be shorter than --since"),
				"As in --since 7d --until 1d for the events from 7 days ago to 1 day ago")
		}
		filters.SetOccurredBefore(timestamppb.New(now.Add(-in.Until)))
		set = true
	}
	if in.Action != "" {
		filters.SetActionPrefix(in.Action)
		set = true
	}
	if in.Resource != "" {
		kind, id, hasID := strings.Cut(in.Resource, "/")
		if kind == "" || (hasID && id == "") {
			return nil, invalidFilter(fmt.Errorf("--resource %q: write KIND or KIND/ID", in.Resource),
				"As in --resource package_rule or --resource policy_change/18")
		}
		filters.SetResourceKind(kind)
		if hasID {
			filters.SetResourceId(id)
		}
		set = true
	}
	if in.Actor != "" {
		filters.SetActor(in.Actor)
		set = true
	}

	req := &auditv1.ListAuditEventsRequest{}
	req.SetPagination(paging.NewPaginationRequest(in.PageSize, in.PageToken))
	if set {
		req.SetFilters(filters)
	}
	return req, nil
}

func runList(ctx context.Context, client eventLister, in listInput, now time.Time) (*listResult, error) {
	req, err := newListRequest(in, now)
	if err != nil {
		return nil, err
	}
	res, err := client.ListAuditEvents(ctx, req)
	if err != nil {
		return nil, mapListError(err)
	}
	out := &listResult{
		Events:        make([]eventView, 0, len(res.GetEvents())),
		NextPageToken: res.GetPagination().GetNextPageToken(),
	}
	for _, e := range res.GetEvents() {
		out.Events = append(out.Events, newEventView(e))
	}
	return out, nil
}

func mapListError(err error) error {
	cause := fmt.Errorf("audit event list: %w", err)
	if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
		return usefulerror.NewUsefulError().
			WithCode(usefulerror.ErrAuthorizationFailed).
			WithHumanError("Only an Admin can read the audit log").
			WithHelp("Ask an Admin of the tenant to run the command").
			Wrap(cause)
	}
	return cause
}

func newEventView(e *auditv1.AuditEvent) eventView {
	v := eventView{
		ID:         e.GetEventId(),
		Actor:      actorName(e.GetActor()),
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

func actorName(a *messagescontroltowerv1.Actor) string {
	if a.GetDisplayName() != "" {
		return a.GetDisplayName()
	}
	return a.GetId()
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
		Rows(rows...).
		EmptyMessage("No audit events match. Try a wider --since window.")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), plural(len(rows)))
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
		fmt.Fprintf(&b, "\n%s\t%s\t%s\t%s\t%s\t%s\t%s", e.OccurredAt, e.Actor, e.Action, e.Kind, e.ResourceID, e.Result, e.detail())
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return "event"
	}
	return "events"
}
