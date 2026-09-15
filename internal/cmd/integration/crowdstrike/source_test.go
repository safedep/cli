package crowdstrike

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	ctmsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	ctsvcv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/cli/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newTestKV opens a temp SQLite-backed KV used by cursorStore, isolated per test.
func newTestKV(t *testing.T) *storage.KV[cursorState] {
	t.Helper()
	s, err := storage.Open(context.Background(), storage.Options{
		Backend: storage.BackendSqlite,
		Path:    filepath.Join(t.TempDir(), "test.db"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	kv, err := storage.NewProfileKV[cursorState](s, "default", "test-cursor")
	require.NoError(t, err)
	return kv
}

// fakeEndpointClient is a hand-rolled stand-in for the gRPC client. Tests queue
// per-call responses and inspect the captured requests after syncOnce returns.
type fakeEndpointClient struct {
	queue    []fakeEventsResp
	captured []*ctsvcv1.ListEndpointPackageGuardEventsRequest
}

type fakeEventsResp struct {
	resp *ctsvcv1.ListEndpointPackageGuardEventsResponse
	err  error
}

var _ controltowerv1grpc.EndpointManagementServiceClient = (*fakeEndpointClient)(nil)

func (f *fakeEndpointClient) ListEndpointPackageGuardEvents(_ context.Context, in *ctsvcv1.ListEndpointPackageGuardEventsRequest, _ ...grpc.CallOption) (*ctsvcv1.ListEndpointPackageGuardEventsResponse, error) {
	f.captured = append(f.captured, in)
	if len(f.queue) == 0 {
		return nil, errors.New("fake: no more queued responses")
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r.resp, r.err
}

// The remaining methods exist only so the fake satisfies the interface.
func (f *fakeEndpointClient) GetEndpointsStats(_ context.Context, _ *ctsvcv1.GetEndpointsStatsRequest, _ ...grpc.CallOption) (*ctsvcv1.GetEndpointsStatsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeEndpointClient) ListEndpoints(_ context.Context, _ *ctsvcv1.ListEndpointsRequest, _ ...grpc.CallOption) (*ctsvcv1.ListEndpointsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeEndpointClient) GetEndpoint(_ context.Context, _ *ctsvcv1.GetEndpointRequest, _ ...grpc.CallOption) (*ctsvcv1.GetEndpointResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeEndpointClient) ListEndpointInventoryEvents(_ context.Context, _ *ctsvcv1.ListEndpointInventoryEventsRequest, _ ...grpc.CallOption) (*ctsvcv1.ListEndpointInventoryEventsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeEndpointClient) ListEndpointAdvisorEvents(_ context.Context, _ *ctsvcv1.ListEndpointAdvisorEventsRequest, _ ...grpc.CallOption) (*ctsvcv1.ListEndpointAdvisorEventsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeEndpointClient) GetEndpointInventorySnapshot(_ context.Context, _ *ctsvcv1.GetEndpointInventorySnapshotRequest, _ ...grpc.CallOption) (*ctsvcv1.GetEndpointInventorySnapshotResponse, error) {
	return nil, errors.New("not implemented in fake")
}

// eventSpec describes one event in a queued page.
type eventSpec struct {
	id           string
	name         string
	version      string
	action       ctmsgv1.PmgPackageAction
	isMalware    bool
	tsOffset     time.Duration // timestamp = base + tsOffset
	skipTS       bool
	cooldownDays uint32
}

func blocked(id, name, version string, tsOffset time.Duration) eventSpec {
	return eventSpec{id: id, name: name, version: version, action: ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED, isMalware: true, tsOffset: tsOffset}
}

func newTestEvent(base time.Time, s eventSpec) *packageGuardEvent {
	pkg := &packagev1.Package{}
	pkg.SetName(s.name)
	pkg.SetEcosystem(packagev1.Ecosystem_ECOSYSTEM_NPM)

	pv := &packagev1.PackageVersion{}
	pv.SetPackage(pkg)
	pv.SetVersion(s.version)

	dec := &ctmsgv1.PmgPackageDecision{}
	dec.SetPackageVersion(pv)
	dec.SetAction(s.action)
	dec.SetIsMalware(s.isMalware)
	if s.action == ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED {
		cd := &ctmsgv1.PmgDependencyCooldown{}
		cd.SetDaysRemaining(s.cooldownDays)
		dec.SetCooldown(cd)
	}

	pe := &ctmsgv1.PmgEvent{}
	pe.SetEventType(ctmsgv1.PmgEventType_PMG_EVENT_TYPE_PACKAGE_DECISION)
	pe.SetPackageDecision(dec)

	ev := &packageGuardEvent{}
	ev.SetEventId(s.id)
	ev.SetEndpointName("host-1")
	ev.SetToolName("pmg")
	ev.SetPmgEvent(pe)
	if !s.skipTS {
		ev.SetTimestamp(timestamppb.New(base.Add(s.tsOffset)))
	}
	return ev
}

func makeEventsPage(base time.Time, nextToken string, specs ...eventSpec) *ctsvcv1.ListEndpointPackageGuardEventsResponse {
	resp := &ctsvcv1.ListEndpointPackageGuardEventsResponse{}
	events := make([]*packageGuardEvent, 0, len(specs))
	for _, s := range specs {
		events = append(events, newTestEvent(base, s))
	}
	resp.SetEvents(events)

	pag := &ctmsgv1.PaginationResponse{}
	pag.SetNextPageToken(nextToken)
	resp.SetPagination(pag)
	return resp
}

func drainHandler() (recordHandler, *[]string) {
	got := &[]string{}
	return func(ev *packageGuardEvent) error {
		*got = append(*got, ev.GetEventId())
		return nil
	}, got
}

func startTime(req *ctsvcv1.ListEndpointPackageGuardEventsRequest) time.Time {
	t := req.GetTimeRange().GetStart()
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func TestSyncOnce_FirstRun_StartIsNowWithZeroBackfill(t *testing.T) {
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{resp: makeEventsPage(time.Now().UTC(), "")}}}
	src := newEventSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	before := time.Now().UTC()
	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))
	after := time.Now().UTC()

	require.Len(t, fake.captured, 1)
	start := startTime(fake.captured[0])
	require.False(t, start.IsZero(), "start must never be omitted; that would pull full history")
	assert.False(t, start.Before(before.Add(-time.Second)), "fresh start uses now")
	assert.False(t, start.After(after.Add(time.Second)), "fresh start uses now")
}

func TestSyncOnce_FirstRun_BackfillSeedsStart(t *testing.T) {
	backfill := 24 * time.Hour
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{resp: makeEventsPage(time.Now().UTC(), "")}}}
	src := newEventSource(fake, newTestKV(t), time.Minute, backfill, newReporter(nil))

	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	start := startTime(fake.captured[0])
	want := time.Now().UTC().Add(-backfill)
	delta := start.Sub(want)
	if delta < 0 {
		delta = -delta
	}
	assert.Less(t, delta, 5*time.Second, "start must be ~ now - backfill; got %v want ~%v", start, want)
}

func TestSyncOnce_RequestShape_BlockActionsAscendingPaged(t *testing.T) {
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{resp: makeEventsPage(time.Now().UTC(), "")}}}
	src := newEventSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	req := fake.captured[0]
	pmg := req.GetFilter().GetPmg()
	assert.Equal(t, []ctmsgv1.PmgEventType{ctmsgv1.PmgEventType_PMG_EVENT_TYPE_PACKAGE_DECISION}, pmg.GetEventTypes())
	assert.Equal(t, []ctmsgv1.PmgPackageAction{
		ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED,
		ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED,
	}, pmg.GetPackageActions(), "only the two block actions are requested server-side")
	assert.Equal(t, uint32(eventPageSize), req.GetPagination().GetPageSize())
	assert.Equal(t, ctmsgv1.PaginationRequest_SORT_ORDER_ASCENDING, req.GetPagination().GetSortOrder(),
		"ascending order is required for reliable incremental sync")
	assert.Empty(t, req.GetPagination().GetPageToken(), "first page has no token")

	// The API requires end and rejects start >= end.
	require.NotNil(t, req.GetTimeRange().GetEnd(), "end is required by the API")
	assert.True(t, req.GetTimeRange().GetEnd().AsTime().After(startTime(req)), "start must be before end")
}

func TestSyncOnce_DeliversEvents_AdvancesCursorToMaxTimestamp(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{resp: makeEventsPage(base, "",
		blocked("a", "pkg-a", "1.0.0", 0),
		blocked("b", "pkg-b", "2.0.0", time.Second),
		blocked("c", "pkg-c", "3.0.0", 2*time.Second),
	)}}}

	store := newCursorStore(newTestKV(t))
	src := &eventSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, got := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	assert.Equal(t, []string{"a", "b", "c"}, *got, "all events delivered in order")

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	want := base.Add(2 * time.Second)
	assert.True(t, saved.LastSeenAt.Equal(want), "cursor advanced to max timestamp; got %v want %v", saved.LastSeenAt, want)
	assert.Equal(t, []string{"c"}, saved.LastSeenEventIDs, "boundary ids are the events at the max timestamp")
}

func TestSyncOnce_MultiPage_StartConstantTokensAdvance(t *testing.T) {
	kv := newTestKV(t)
	cursor := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	store := newCursorStore(kv)
	require.NoError(t, store.save(context.Background(), cursorState{LastSeenAt: cursor}))

	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeEndpointClient{queue: []fakeEventsResp{
		{resp: makeEventsPage(base, "tok1", blocked("a", "pkg-a", "1.0", 0))},
		{resp: makeEventsPage(base, "tok2", blocked("b", "pkg-b", "2.0", time.Hour))},
		{resp: makeEventsPage(base, "", blocked("c", "pkg-c", "3.0", 2*time.Hour))},
	}}

	src := &eventSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, got := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	assert.Equal(t, []string{"a", "b", "c"}, *got, "events delivered across all 3 pages")
	require.Len(t, fake.captured, 3, "exactly 3 page requests were made")

	for i, req := range fake.captured {
		assert.True(t, startTime(req).Equal(cursor),
			"page %d start drifted: got %v want %v", i, startTime(req), cursor)
	}
	assert.Empty(t, fake.captured[0].GetPagination().GetPageToken())
	assert.Equal(t, "tok1", fake.captured[1].GetPagination().GetPageToken())
	assert.Equal(t, "tok2", fake.captured[2].GetPagination().GetPageToken())
}

func TestSyncOnce_BoundaryDedup_SkipsSeenIDs(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)

	store := newCursorStore(newTestKV(t))
	// Cursor sits at base with event "a" already processed at that timestamp.
	require.NoError(t, store.save(context.Background(), cursorState{LastSeenAt: base, LastSeenEventIDs: []string{"a"}}))

	// The server re-delivers "a" at base (inclusive Start) plus a new "b" at base+1s.
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{resp: makeEventsPage(base, "",
		blocked("a", "pkg-a", "1.0.0", 0),
		blocked("b", "pkg-b", "2.0.0", time.Second),
	)}}}
	src := &eventSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, got := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	assert.Equal(t, []string{"b"}, *got, "the already-seen boundary event is skipped, not re-delivered")

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	assert.True(t, saved.LastSeenAt.Equal(base.Add(time.Second)), "cursor advanced past the new event")
	assert.Equal(t, []string{"b"}, saved.LastSeenEventIDs)
}

func TestSyncOnce_FirstRunNoEvents_AnchorsCursor(t *testing.T) {
	fake := &fakeEndpointClient{queue: []fakeEventsResp{
		{resp: makeEventsPage(time.Now().UTC(), "")},
		{resp: makeEventsPage(time.Now().UTC(), "")},
	}}
	store := newCursorStore(newTestKV(t))
	src := &eventSource{svc: fake, cursor: store, pollInterval: time.Minute, backfillWindow: 24 * time.Hour, rep: newReporter(nil)}

	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	require.False(t, saved.LastSeenAt.IsZero(), "first-run anchor must persist even with zero events")
	anchor := saved.LastSeenAt

	require.NoError(t, src.syncOnce(context.Background(), handler))
	require.Len(t, fake.captured, 2)
	assert.True(t, startTime(fake.captured[1]).Equal(anchor),
		"start must stay anchored across empty cycles; got %v want %v", startTime(fake.captured[1]), anchor)
}

func TestSyncOnce_PermissionDenied_ReturnsNotEntitled(t *testing.T) {
	entErr := status.Error(codes.PermissionDenied, "required entitlement is not available for tenant")
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{err: entErr}}}
	src := newEventSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	handler, _ := drainHandler()
	err := src.syncOnce(context.Background(), handler)
	require.ErrorIs(t, err, errNotEntitled, "PermissionDenied maps to the friendly add-on error")
}

func TestSyncOnce_AuthFailure_ReturnsAuthError(t *testing.T) {
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{err: status.Error(codes.Unauthenticated, "missing or expired credentials")}}}
	src := newEventSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	handler, _ := drainHandler()
	err := src.syncOnce(context.Background(), handler)
	require.ErrorIs(t, err, errAuth, "an Unauthenticated status maps to the friendly auth error")
}

func TestSyncOnce_CallbackError_StopsAndWraps(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{resp: makeEventsPage(base, "",
		blocked("a", "pkg-a", "1.0", 0),
		blocked("b", "pkg-b", "2.0", time.Second),
	)}}}

	src := &eventSource{svc: fake, cursor: newCursorStore(newTestKV(t)), pollInterval: time.Minute, rep: newReporter(nil)}

	stop := errors.New("callback bailed")
	delivered := 0
	err := src.syncOnce(context.Background(), func(*packageGuardEvent) error {
		delivered++
		return stop
	})

	require.Error(t, err)
	assert.True(t, isCallbackError(err), "handler error must be wrapped as a callbackError")
	require.ErrorIs(t, err, stop)
	assert.Equal(t, 1, delivered, "callback error stops delivery immediately")
}

func TestSubscribe_NotEntitled_StopsImmediately(t *testing.T) {
	entErr := status.Error(codes.PermissionDenied, "required entitlement is not available for tenant")
	fake := &fakeEndpointClient{queue: []fakeEventsResp{{err: entErr}}}
	// Long interval so a wrongly-retrying implementation would hang the test.
	src := newEventSource(fake, newTestKV(t), time.Hour, 0, newReporter(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handler, _ := drainHandler()
	err := src.subscribe(ctx, handler)

	require.ErrorIs(t, err, errNotEntitled, "the add-on error must surface from subscribe")
	require.NoError(t, ctx.Err(), "subscribe must stop immediately, not wait for the interval")
	assert.Len(t, fake.captured, 1, "must not retry after an entitlement failure")
}

func TestSubscribe_InfraError_LoggedAndRetried(t *testing.T) {
	fake := &fakeEndpointClient{queue: []fakeEventsResp{
		{err: errors.New("grpc unavailable")},
		{resp: makeEventsPage(time.Now().UTC(), "")},
	}}
	src := newEventSource(fake, newTestKV(t), 10*time.Millisecond, 0, newReporter(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	handler, _ := drainHandler()
	err := src.subscribe(ctx, handler)

	require.NoError(t, err, "infra errors must NOT surface; they are logged and retried")
	assert.GreaterOrEqual(t, len(fake.captured), 2, "loop must continue after the first cycle's infra error")
}
