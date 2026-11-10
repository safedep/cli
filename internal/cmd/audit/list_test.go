package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	auditv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/audit/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var now = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// fakeLister is a hand fake for the one RPC of eventLister.
type fakeLister struct {
	req *auditv1.ListAuditEventsRequest
	res *auditv1.ListAuditEventsResponse
	err error
}

func (f *fakeLister) ListAuditEvents(_ context.Context, req *auditv1.ListAuditEventsRequest, _ ...grpc.CallOption) (*auditv1.ListAuditEventsResponse, error) {
	f.req = req
	return f.res, f.err
}

func TestNewListRequest(t *testing.T) {
	tests := []struct {
		name    string
		in      listInput
		check   func(*testing.T, *auditv1.ListAuditEventsRequest_Filters)
		wantErr string
	}{
		{
			name: "no filters",
			in:   listInput{},
			check: func(t *testing.T, f *auditv1.ListAuditEventsRequest_Filters) {
				assert.Nil(t, f)
			},
		},
		{
			name: "every filter",
			in: listInput{
				Since: 7 * 24 * time.Hour, Until: 24 * time.Hour,
				Action: "policy.", Resource: "policy_change/18", Actor: "alice@acme.io",
			},
			check: func(t *testing.T, f *auditv1.ListAuditEventsRequest_Filters) {
				assert.Equal(t, now.Add(-7*24*time.Hour), f.GetOccurredAfter().AsTime())
				assert.Equal(t, now.Add(-24*time.Hour), f.GetOccurredBefore().AsTime())
				assert.Equal(t, "policy.", f.GetActionPrefix())
				assert.Equal(t, "policy_change", f.GetResourceKind())
				assert.Equal(t, "18", f.GetResourceId())
				assert.Equal(t, "alice@acme.io", f.GetActor())
			},
		},
		{
			name: "resource kind only",
			in:   listInput{Resource: "package_rule"},
			check: func(t *testing.T, f *auditv1.ListAuditEventsRequest_Filters) {
				assert.Equal(t, "package_rule", f.GetResourceKind())
				assert.False(t, f.HasResourceId())
			},
		},
		{name: "empty resource id", in: listInput{Resource: "policy_change/"}, wantErr: "write KIND or KIND/ID"},
		{name: "empty resource kind", in: listInput{Resource: "/18"}, wantErr: "write KIND or KIND/ID"},
		{name: "until after since", in: listInput{Since: time.Hour, Until: 2 * time.Hour}, wantErr: "--until must be shorter than --since"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := newListRequest(tt.in, now)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if req.HasFilters() {
				tt.check(t, req.GetFilters())
			} else {
				tt.check(t, nil)
			}
		})
	}
}

func auditEvent() *auditv1.AuditEvent {
	actor := &messagescontroltowerv1.Actor{}
	actor.SetKind(messagescontroltowerv1.ActorKind_ACTOR_KIND_USER)
	actor.SetId("u_1")
	actor.SetDisplayName("alice")
	resource := &auditv1.AuditResource{}
	resource.SetKind("package_rule")
	resource.SetId("r_80b")
	details, err := structpb.NewStruct(map[string]any{"revision": 51, "reason": "no longer needed"})
	if err != nil {
		panic(err)
	}
	e := &auditv1.AuditEvent{}
	e.SetEventId("ev_1")
	e.SetActor(actor)
	e.SetAction("policy.rule.delete")
	e.SetResource(resource)
	e.SetResult(auditv1.AuditResult_AUDIT_RESULT_SUCCESS)
	e.SetDetails(details)
	e.SetOccurredAt(timestamppb.New(time.Now().Add(-10 * time.Minute)))
	return e
}

func TestRunList(t *testing.T) {
	res := &auditv1.ListAuditEventsResponse{}
	res.SetEvents([]*auditv1.AuditEvent{auditEvent()})
	client := &fakeLister{res: res}

	got, err := runList(context.Background(), client, listInput{Action: "policy.", PageSize: 20}, now)
	require.NoError(t, err)
	assert.Equal(t, uint32(20), client.req.GetPagination().GetPageSize())
	require.Len(t, got.Events, 1)

	table := got.RenderTable()
	for _, want := range []string{"10m ago", "alice", "policy.rule.delete", "package_rule r_80b", "success", "reason=no longer needed, revision=51"} {
		assert.Contains(t, table, want)
	}
	assert.Contains(t, got.RenderPlain(), "\talice\tpolicy.rule.delete\tpackage_rule\tr_80b\tsuccess\t")
	b, err := got.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"actor_kind": "user"`)
}

func TestRunList_MemberIsDenied(t *testing.T) {
	client := &fakeLister{err: status.Error(codes.PermissionDenied, "member")}
	_, err := runList(context.Background(), client, listInput{}, now)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, "Only an Admin can read the audit log", ue.HumanError())
}

func TestRunList_ShowsWhyTheServerRefusedTheRequest(t *testing.T) {
	client := &fakeLister{err: status.Error(codes.InvalidArgument,
		`service execution failed: invalid audit event list request: audit: unknown resource kind: "bogus"`)}
	_, err := runList(context.Background(), client, listInput{Resource: "bogus"}, now)
	ue, ok := usefulerror.AsUsefulError(err)
	require.True(t, ok)
	assert.Equal(t, usefulerror.ErrBadRequest, ue.Code())
	assert.Equal(t, `The server refused the request: invalid audit event list request: audit: unknown resource kind: "bogus"`, ue.HumanError())
	assert.Equal(t, "See the filters: safedep audit event list --help", ue.Help())
}

func TestRender_EscapesServerText(t *testing.T) {
	e := auditEvent()
	e.GetActor().SetDisplayName("mallory\x1b]0;owned\x07")
	details, err := structpb.NewStruct(map[string]any{"reason": "a\nb"})
	require.NoError(t, err)
	e.SetDetails(details)
	res := &auditv1.ListAuditEventsResponse{}
	res.SetEvents([]*auditv1.AuditEvent{e})

	got, err := runList(context.Background(), &fakeLister{res: res}, listInput{}, now)
	require.NoError(t, err)
	for _, out := range []string{got.RenderTable(), got.RenderPlain()} {
		assert.NotContains(t, out, "\x1b")
		assert.Contains(t, out, `mallory\x1b]0;owned\x07`)
		assert.Contains(t, out, `reason=a\nb`)
	}
	assert.Len(t, strings.Split(got.RenderPlain(), "\n"), 2, "a line break in a value adds no row")
}
