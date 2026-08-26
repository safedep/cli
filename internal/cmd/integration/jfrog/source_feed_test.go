package jfrog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	threatintelv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/threatintel/v1/threatintelv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	threatintelsvcv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/threatintel/v1"
	"github.com/safedep/cli/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newTestKV opens a temp SQLite-backed KV used by cursorStore. Each call
// returns an isolated KV in its own DB so tests cannot interfere with
// each other or with the user's real state.
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

// fakeThreatIntelClient is a hand-rolled stand-in for the gRPC client.
// Tests queue per-call responses and inspect the captured requests after
// syncOnce returns. The variadic grpc.CallOption matches the real signature.
type fakeThreatIntelClient struct {
	queue    []fakeReportsResp
	captured []*threatintelsvcv1.ListPackageReportsRequest
}

type fakeReportsResp struct {
	resp *threatintelsvcv1.ListPackageReportsResponse
	err  error
}

var _ threatintelv1grpc.ThreatIntelServiceClient = (*fakeThreatIntelClient)(nil)

func (f *fakeThreatIntelClient) ListPackageReports(_ context.Context, in *threatintelsvcv1.ListPackageReportsRequest, _ ...grpc.CallOption) (*threatintelsvcv1.ListPackageReportsResponse, error) {
	f.captured = append(f.captured, in)
	if len(f.queue) == 0 {
		return nil, errors.New("fake: no more queued responses")
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r.resp, r.err
}

// The remaining methods are not exercised by the feed source; stubs only
// exist so the fake satisfies the interface.
func (f *fakeThreatIntelClient) GetPackageReport(_ context.Context, _ *threatintelsvcv1.GetPackageReportRequest, _ ...grpc.CallOption) (*threatintelsvcv1.GetPackageReportResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeThreatIntelClient) ListIndicators(_ context.Context, _ *threatintelsvcv1.ListIndicatorsRequest, _ ...grpc.CallOption) (*threatintelsvcv1.ListIndicatorsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeThreatIntelClient) LookupIndicator(_ context.Context, _ *threatintelsvcv1.LookupIndicatorRequest, _ ...grpc.CallOption) (*threatintelsvcv1.LookupIndicatorResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeThreatIntelClient) ListCampaigns(_ context.Context, _ *threatintelsvcv1.ListCampaignsRequest, _ ...grpc.CallOption) (*threatintelsvcv1.ListCampaignsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeThreatIntelClient) GetCampaign(_ context.Context, _ *threatintelsvcv1.GetCampaignRequest, _ ...grpc.CallOption) (*threatintelsvcv1.GetCampaignResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeThreatIntelClient) ListSnapshots(_ context.Context, _ *threatintelsvcv1.ListSnapshotsRequest, _ ...grpc.CallOption) (*threatintelsvcv1.ListSnapshotsResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeThreatIntelClient) GetSnapshotDownloadUrl(_ context.Context, _ *threatintelsvcv1.GetSnapshotDownloadUrlRequest, _ ...grpc.CallOption) (*threatintelsvcv1.GetSnapshotDownloadUrlResponse, error) {
	return nil, errors.New("not implemented in fake")
}

// reportSpec describes one report in a queued page.
type reportSpec struct {
	id            string
	name          string
	versions      []string
	updatedOffset time.Duration // updated_at = baseTime + updatedOffset
	withdrawn     bool
	skipUpdatedAt bool
}

// makeReportsPage builds a ListPackageReportsResponse. nextToken sets the
// pagination's next_page_token.
func makeReportsPage(baseTime time.Time, nextToken string, specs ...reportSpec) *threatintelsvcv1.ListPackageReportsResponse {
	resp := &threatintelsvcv1.ListPackageReportsResponse{}
	reports := make([]*threatintelv1.PackageReport, 0, len(specs))
	for _, s := range specs {
		r := newTestReport(s.id, s.name, packagev1.Ecosystem_ECOSYSTEM_NPM, s.versions...)
		r.SetWithdrawn(s.withdrawn)
		if !s.skipUpdatedAt {
			r.SetUpdatedAt(timestamppb.New(baseTime.Add(s.updatedOffset)))
		}
		reports = append(reports, r)
	}
	resp.SetPackageReports(reports)

	pag := &controltowerv1.PaginationResponse{}
	pag.SetNextPageToken(nextToken)
	resp.SetPagination(pag)
	return resp
}

// drainHandler returns a recordHandler that records every delivered
// report id in order, useful for asserting delivery sequence.
func drainHandler() (recordHandler, *[]string) {
	got := &[]string{}
	return func(r *threatintelv1.PackageReport) error {
		*got = append(*got, r.GetReportId())
		return nil
	}, got
}

// sinceTime pulls filters.since out of a captured request.
func sinceTime(req *threatintelsvcv1.ListPackageReportsRequest) time.Time {
	t := req.GetFilters().GetSince()
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func TestSyncOnce_FirstRun_SinceIsNowWithZeroBackfill(t *testing.T) {
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(time.Now().UTC(), "")}}}
	src := newFeedSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	before := time.Now().UTC()
	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))
	after := time.Now().UTC()

	require.Len(t, fake.captured, 1)
	since := sinceTime(fake.captured[0])
	require.False(t, since.IsZero(), "since must never be omitted; that would pull full history")
	assert.False(t, since.Before(before.Add(-time.Second)), "fresh start uses since ~ now")
	assert.False(t, since.After(after.Add(time.Second)), "fresh start uses since ~ now")
}

func TestSyncOnce_FirstRun_BackfillSeedsSince(t *testing.T) {
	backfill := 24 * time.Hour
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(time.Now().UTC(), "")}}}
	src := newFeedSource(fake, newTestKV(t), time.Minute, backfill, newReporter(nil))

	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	since := sinceTime(fake.captured[0])
	want := time.Now().UTC().Add(-backfill)
	delta := since.Sub(want)
	if delta < 0 {
		delta = -delta
	}
	assert.Less(t, delta, 5*time.Second, "since must be ~ now - backfill; got %v want ~%v", since, want)
}

func TestSyncOnce_RequestShape_MaliciousAscendingPaged(t *testing.T) {
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(time.Now().UTC(), "")}}}
	src := newFeedSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	req := fake.captured[0]
	assert.Equal(t, threatintelv1.ThreatVerdict_THREAT_VERDICT_MALICIOUS, req.GetFilters().GetVerdict(),
		"suspicious is dropped server-side via the verdict filter")
	assert.Equal(t, uint32(feedPageSize), req.GetPagination().GetPageSize())
	assert.Equal(t, controltowerv1.PaginationRequest_SORT_ORDER_ASCENDING, req.GetPagination().GetSortOrder(),
		"ascending order is required for reliable incremental sync")
	assert.Empty(t, req.GetPagination().GetPageToken(), "first page has no token")
}

func TestSyncOnce_DeliversReports_AdvancesCursorToMaxUpdatedAt(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(base, "",
		reportSpec{id: "a", name: "pkg-a", versions: []string{"1.0.0"}, updatedOffset: 0},
		reportSpec{id: "b", name: "pkg-b", versions: []string{"2.0.0"}, updatedOffset: time.Second},
		reportSpec{id: "c", name: "pkg-c", versions: []string{"3.0.0"}, updatedOffset: 2 * time.Second},
	)}}}

	store := newCursorStore(newTestKV(t))
	src := &feedSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, got := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	assert.Equal(t, []string{"a", "b", "c"}, *got, "all reports delivered in order")

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	want := base.Add(2 * time.Second) // report c, the latest
	assert.True(t, saved.LastSeenAt.Equal(want), "cursor advanced to max updated_at; got %v want %v", saved.LastSeenAt, want)
}

func TestSyncOnce_MultiPage_SinceConstantTokensAdvance(t *testing.T) {
	kv := newTestKV(t)
	cursor := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	store := newCursorStore(kv)
	require.NoError(t, store.save(context.Background(), cursorState{LastSeenAt: cursor}))

	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{
		{resp: makeReportsPage(base, "tok1", reportSpec{id: "a", name: "pkg-a", versions: []string{"1.0"}, updatedOffset: 0})},
		{resp: makeReportsPage(base, "tok2", reportSpec{id: "b", name: "pkg-b", versions: []string{"2.0"}, updatedOffset: time.Hour})},
		{resp: makeReportsPage(base, "", reportSpec{id: "c", name: "pkg-c", versions: []string{"3.0"}, updatedOffset: 2 * time.Hour})},
	}}

	src := &feedSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, got := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	assert.Equal(t, []string{"a", "b", "c"}, *got, "reports delivered across all 3 pages")
	require.Len(t, fake.captured, 3, "exactly 3 page requests were made")

	// since is fixed for the whole drain; only the page token moves forward.
	for i, req := range fake.captured {
		assert.True(t, sinceTime(req).Equal(cursor),
			"page %d since drifted: got %v want %v", i, sinceTime(req), cursor)
	}
	assert.Empty(t, fake.captured[0].GetPagination().GetPageToken())
	assert.Equal(t, "tok1", fake.captured[1].GetPagination().GetPageToken())
	assert.Equal(t, "tok2", fake.captured[2].GetPagination().GetPageToken())
}

func TestSyncOnce_WithdrawnDelivered_CursorAdvancesPastIt(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(base, "",
		reportSpec{id: "a", name: "pkg-a", versions: []string{"1.0.0"}, updatedOffset: 0},
		reportSpec{id: "b", name: "pkg-b", withdrawn: true, updatedOffset: 5 * time.Second},
	)}}}

	store := newCursorStore(newTestKV(t))
	src := &feedSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, got := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	// The source does not drop withdrawn reports; feedService decides.
	assert.Equal(t, []string{"a", "b"}, *got, "withdrawn report is delivered, not dropped at the source")

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	want := base.Add(5 * time.Second) // the withdrawn report is the latest
	assert.True(t, saved.LastSeenAt.Equal(want), "cursor must advance past a withdrawn report; got %v want %v", saved.LastSeenAt, want)
}

func TestSyncOnce_GRPCFailure_PropagatesAndKeepsCursor(t *testing.T) {
	store := newCursorStore(newTestKV(t))
	original := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Microsecond)
	require.NoError(t, store.save(context.Background(), cursorState{LastSeenAt: original}))

	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{err: errors.New("grpc unavailable")}}}
	src := &feedSource{svc: fake, cursor: store, pollInterval: time.Minute, rep: newReporter(nil)}

	handler, _ := drainHandler()
	err := src.syncOnce(context.Background(), handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list reports")

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	assert.True(t, saved.LastSeenAt.Equal(original), "cursor must not advance after a gRPC error; got %v want %v", saved.LastSeenAt, original)
}

func TestSyncOnce_FirstRunNoReports_AnchorsCursor(t *testing.T) {
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{
		{resp: makeReportsPage(time.Now().UTC(), "")}, // cycle 1: empty
		{resp: makeReportsPage(time.Now().UTC(), "")}, // cycle 2: empty
	}}
	store := newCursorStore(newTestKV(t))
	src := &feedSource{svc: fake, cursor: store, pollInterval: time.Minute, backfillWindow: 24 * time.Hour, rep: newReporter(nil)}

	handler, _ := drainHandler()
	require.NoError(t, src.syncOnce(context.Background(), handler))

	saved, err := store.load(context.Background())
	require.NoError(t, err)
	require.False(t, saved.LastSeenAt.IsZero(), "first-run anchor must persist even with zero reports")
	anchor := saved.LastSeenAt

	// A second empty drain must resume from the same anchor, not slide forward.
	require.NoError(t, src.syncOnce(context.Background(), handler))
	require.Len(t, fake.captured, 2)
	assert.True(t, sinceTime(fake.captured[1]).Equal(anchor),
		"since must stay anchored across empty cycles; got %v want %v", sinceTime(fake.captured[1]), anchor)

	saved2, err := store.load(context.Background())
	require.NoError(t, err)
	assert.True(t, saved2.LastSeenAt.Equal(anchor), "cursor must not slide on the second empty cycle")
}

func TestSyncOnce_PermissionDenied_ReturnsNotEntitled(t *testing.T) {
	entErr := status.Error(codes.PermissionDenied,
		"entitlement verification failed: required entitlement is not available for tenant")
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{err: entErr}}}
	src := newFeedSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

	handler, _ := drainHandler()
	err := src.syncOnce(context.Background(), handler)
	require.ErrorIs(t, err, errFeedNotEntitled, "PermissionDenied maps to the friendly add-on error")
}

func TestSubscribe_NotEntitled_StopsImmediately(t *testing.T) {
	entErr := status.Error(codes.PermissionDenied, "required entitlement is not available for tenant")
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{err: entErr}}}

	// Long interval so a wrongly-retrying implementation would hang the test.
	src := newFeedSource(fake, newTestKV(t), time.Hour, 0, newReporter(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handler, _ := drainHandler()
	err := src.subscribe(ctx, handler)

	require.ErrorIs(t, err, errFeedNotEntitled, "the add-on error must surface from subscribe")
	require.NoError(t, ctx.Err(), "subscribe must stop immediately, not wait for the interval")
	assert.Len(t, fake.captured, 1, "must not retry after an entitlement failure")
}

func TestSyncOnce_AuthFailure_ReturnsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "internal status naming the api key",
			err:  status.Error(codes.Internal, "unauthenticated: api key auth failed: api key does not belong to tenant"),
		},
		{
			name: "unauthenticated status",
			err:  status.Error(codes.Unauthenticated, "missing credentials"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{err: tt.err}}}
			src := newFeedSource(fake, newTestKV(t), time.Minute, 0, newReporter(nil))

			handler, _ := drainHandler()
			err := src.syncOnce(context.Background(), handler)
			require.ErrorIs(t, err, errFeedAuth, "a data-plane auth failure maps to the friendly auth error")
		})
	}
}

func TestSubscribe_AuthFailure_StopsImmediately(t *testing.T) {
	authErr := status.Error(codes.Internal, "unauthenticated: api key auth failed: api key does not belong to tenant")
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{err: authErr}}}

	// Long interval so a wrongly-retrying implementation would hang the test.
	src := newFeedSource(fake, newTestKV(t), time.Hour, 0, newReporter(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handler, _ := drainHandler()
	err := src.subscribe(ctx, handler)

	require.ErrorIs(t, err, errFeedAuth, "the auth error must surface from subscribe")
	require.NoError(t, ctx.Err(), "subscribe must stop immediately, not wait for the interval")
	assert.Len(t, fake.captured, 1, "must not retry after an auth failure")
}

func TestSyncOnce_CallbackError_StopsAndWraps(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(base, "",
		reportSpec{id: "a", name: "pkg-a", versions: []string{"1.0"}, updatedOffset: 0},
		reportSpec{id: "b", name: "pkg-b", versions: []string{"2.0"}, updatedOffset: time.Second},
	)}}}

	src := &feedSource{svc: fake, cursor: newCursorStore(newTestKV(t)), pollInterval: time.Minute, rep: newReporter(nil)}

	stop := errors.New("callback bailed")
	delivered := 0
	err := src.syncOnce(context.Background(), func(*threatintelv1.PackageReport) error {
		delivered++
		return stop
	})

	require.Error(t, err)
	assert.True(t, isCallbackError(err), "handler error must be wrapped as a callbackError")
	require.ErrorIs(t, err, stop)
	assert.Equal(t, 1, delivered, "callback error stops delivery immediately on the first report")
}

// TestSubscribe_CallbackError_PropagatesImmediately verifies the contract
// from source.go: a recordHandler error must surface from subscribe (not
// be logged as a transient and retried on the next cycle), unwrapped.
func TestSubscribe_CallbackError_PropagatesImmediately(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{{resp: makeReportsPage(base, "",
		reportSpec{id: "a", name: "pkg-a", versions: []string{"1.0"}, updatedOffset: 0},
	)}}}

	// Long interval so a buggy implementation that retries instead of
	// surfacing would obviously hang the test (caught by deadline).
	src := newFeedSource(fake, newTestKV(t), time.Hour, 0, newReporter(nil))

	stop := errors.New("handler said stop")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := src.subscribe(ctx, func(*threatintelv1.PackageReport) error {
		return stop
	})

	require.ErrorIs(t, err, stop, "callback error must surface from subscribe, not be retried")
	require.NoError(t, ctx.Err(), "subscribe must return immediately, not wait for ctx timeout")

	// subscribe must unwrap before returning: callers see the original
	// error, not the source-internal callbackError wrapper.
	var cb *callbackError
	require.False(t, errors.As(err, &cb), "surfaced error must not be the callbackError wrapper")
}

// TestSubscribe_InfraError_LoggedAndRetried is the inverse contract:
// transient infrastructure errors are NOT surfaced; they are logged and
// the loop continues until ctx is cancelled.
func TestSubscribe_InfraError_LoggedAndRetried(t *testing.T) {
	fake := &fakeThreatIntelClient{queue: []fakeReportsResp{
		{err: errors.New("grpc unavailable")},
		{resp: makeReportsPage(time.Now().UTC(), "")},
	}}

	src := newFeedSource(fake, newTestKV(t), 10*time.Millisecond, 0, newReporter(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	handler, _ := drainHandler()
	err := src.subscribe(ctx, handler)

	require.NoError(t, err, "infra errors must NOT surface; they are logged and retried")
	assert.GreaterOrEqual(t, len(fake.captured), 2, "loop must continue after the first cycle's infra error")
}
