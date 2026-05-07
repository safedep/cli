package jfrog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/safedep/cli/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newTestKV opens a temp SQLite-backed KV used by cursorStore. Each call
// returns an isolated KV in its own DB so tests cannot interfere with
// each other or with the user's real state.
func newTestKV(t *testing.T) *storage.KV[time.Time] {
	t.Helper()
	s, err := storage.Open(context.Background(), storage.Options{
		Backend: storage.BackendSqlite,
		Path:    filepath.Join(t.TempDir(), "test.db"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	kv, err := storage.NewProfileKV[time.Time](s, "default", "test-cursor")
	require.NoError(t, err)
	return kv
}

// fakeMalysisClient is a hand-rolled stand-in for the gRPC client. Tests
// queue per-call responses and inspect the captured requests after Poll
// returns. The variadic grpc.CallOption matches the real signature.
type fakeMalysisClient struct {
	queue    []fakeResp
	captured []*malysisv1.ListPackageAnalysisRecordsRequest
}

type fakeResp struct {
	resp *malysisv1.ListPackageAnalysisRecordsResponse
	err  error
}

var _ malysisv1grpc.MalwareAnalysisServiceClient = (*fakeMalysisClient)(nil)

func (f *fakeMalysisClient) ListPackageAnalysisRecords(_ context.Context, in *malysisv1.ListPackageAnalysisRecordsRequest, _ ...grpc.CallOption) (*malysisv1.ListPackageAnalysisRecordsResponse, error) {
	f.captured = append(f.captured, in)
	if len(f.queue) == 0 {
		return nil, errors.New("fake: no more queued responses")
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r.resp, r.err
}

// The remaining methods on MalwareAnalysisServiceClient are not exercised
// by the poller; stubs only exist so the fake satisfies the interface.
func (f *fakeMalysisClient) AnalyzePackage(_ context.Context, _ *malysisv1.AnalyzePackageRequest, _ ...grpc.CallOption) (*malysisv1.AnalyzePackageResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeMalysisClient) GetAnalysisReport(_ context.Context, _ *malysisv1.GetAnalysisReportRequest, _ ...grpc.CallOption) (*malysisv1.GetAnalysisReportResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeMalysisClient) QueryPackageAnalysis(_ context.Context, _ *malysisv1.QueryPackageAnalysisRequest, _ ...grpc.CallOption) (*malysisv1.QueryPackageAnalysisResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeMalysisClient) InternalAnalyzePackage(_ context.Context, _ *malysisv1.InternalAnalyzePackageRequest, _ ...grpc.CallOption) (*malysisv1.InternalAnalyzePackageResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeMalysisClient) InternalAgenticAnalyzePackage(_ context.Context, _ *malysisv1.InternalAgenticAnalyzePackageRequest, _ ...grpc.CallOption) (*malysisv1.InternalAgenticAnalyzePackageResponse, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeMalysisClient) InternalPublishDomainEvent(_ context.Context, _ *malysisv1.InternalPublishDomainEventRequest, _ ...grpc.CallOption) (*malysisv1.InternalPublishDomainEventResponse, error) {
	return nil, errors.New("not implemented in fake")
}

// makePage builds an analysis-records response. Each (name, version) pair
// becomes one record with a created_at offset by its index from baseTime.
// nextToken sets the pagination's next_page_token.
func makePage(baseTime time.Time, nextToken string, records ...recordSpec) *malysisv1.ListPackageAnalysisRecordsResponse {
	resp := &malysisv1.ListPackageAnalysisRecordsResponse{}
	for i, r := range records {
		rec := newTestRecord(r.name, r.version, packagev1.Ecosystem_ECOSYSTEM_NPM)
		rec.SetAnalysisId(r.id)
		if !r.skipCreatedAt {
			rec.SetCreatedAt(timestamppb.New(baseTime.Add(time.Duration(i) * time.Second)))
		}
		resp.SetRecords(append(resp.GetRecords(), rec))
	}
	pag := &controltowerv1.PaginationResponse{}
	pag.SetNextPageToken(nextToken)
	resp.SetPagination(pag)
	return resp
}

type recordSpec struct {
	id            string
	name          string
	version       string
	skipCreatedAt bool
}

// drainHandler returns a recordHandler that records every delivered
// AnalysisId in order, useful for asserting delivery sequence.
func drainHandler(t *testing.T) (recordHandler, *[]string) {
	t.Helper()
	got := &[]string{}
	return func(rec *malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		*got = append(*got, rec.GetAnalysisId())
		return nil
	}, got
}

// startFromTime is a small helper that pulls start_from out of a captured
// request, returning zero-time when unset (the "first run / use server
// default" path).
func startFromTime(req *malysisv1.ListPackageAnalysisRecordsRequest) time.Time {
	t := req.GetStartFrom()
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func TestPoll_FirstRun_NoStartFromOnRequest(t *testing.T) {
	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(time.Now().UTC(), "")}}}
	p := newMaliciousPackagePoller(fake, newCursorStore(newTestKV(t)))

	handler, _ := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	assert.Nil(t, fake.captured[0].GetStartFrom(),
		"first run must omit start_from so server uses now-1h default")
}

func TestPoll_WithCursor_SetsStartFromExactly(t *testing.T) {
	kv := newTestKV(t)
	store := newCursorStore(kv)

	cursor := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, store.Save(context.Background(), cursor))

	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(time.Now().UTC(), "")}}}
	p := newMaliciousPackagePoller(fake, store)

	handler, _ := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	got := startFromTime(fake.captured[0])
	assert.True(t, got.Equal(cursor),
		"start_from must equal the loaded cursor; got %v want %v", got, cursor)
}

func TestPoll_FilterAlwaysOnlyVerifiedMalware(t *testing.T) {
	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(time.Now().UTC(), "")}}}
	p := newMaliciousPackagePoller(fake, newCursorStore(newTestKV(t)))

	handler, _ := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	f := fake.captured[0].GetFilter()
	require.NotNil(t, f)
	assert.True(t, f.GetOnlyMalware(), "filter.only_malware must be true")
	assert.True(t, f.GetOnlyVerified(), "filter.only_verified must be true")
}

func TestPoll_DeliversRecords_AdvancesCursorToMaxCreatedAt(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(base, "",
		recordSpec{id: "a", name: "pkg-a", version: "1.0.0"},
		recordSpec{id: "b", name: "pkg-b", version: "2.0.0"},
		recordSpec{id: "c", name: "pkg-c", version: "3.0.0"},
	)}}}

	store := newCursorStore(newTestKV(t))
	p := newMaliciousPackagePoller(fake, store)

	handler, got := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	assert.Equal(t, []string{"a", "b", "c"}, *got, "all records delivered in order")

	saved, err := store.Load(context.Background())
	require.NoError(t, err)
	want := base.Add(2 * time.Second) // record c, the latest
	assert.True(t, saved.Equal(want), "cursor advanced to max created_at; got %v want %v", saved, want)
}

func TestPoll_MultiPage_StartFromConstantAcrossPages(t *testing.T) {
	kv := newTestKV(t)
	cursor := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	store := newCursorStore(kv)
	require.NoError(t, store.Save(context.Background(), cursor))

	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeMalysisClient{queue: []fakeResp{
		{resp: makePage(base, "tok1", recordSpec{id: "a", name: "pkg-a", version: "1.0"})},
		{resp: makePage(base.Add(time.Hour), "tok2", recordSpec{id: "b", name: "pkg-b", version: "2.0"})},
		{resp: makePage(base.Add(2*time.Hour), "", recordSpec{id: "c", name: "pkg-c", version: "3.0"})},
	}}

	p := newMaliciousPackagePoller(fake, store)

	handler, got := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	assert.Equal(t, []string{"a", "b", "c"}, *got, "records delivered across all 3 pages")
	require.Len(t, fake.captured, 3, "exactly 3 page requests were made")

	// The whole point of sessionStartFrom: every page in one session
	// uses the same start_from. next_page_token is what moves forward.
	for i, req := range fake.captured {
		assert.True(t, startFromTime(req).Equal(cursor),
			"page %d start_from drifted: got %v want %v", i, startFromTime(req), cursor)
	}
	assert.Empty(t, fake.captured[0].GetPagination().GetPageToken(), "first page has no token")
	assert.Equal(t, "tok1", fake.captured[1].GetPagination().GetPageToken())
	assert.Equal(t, "tok2", fake.captured[2].GetPagination().GetPageToken())
}

func TestPoll_StaleCursor_ResetsToSafeWindow(t *testing.T) {
	kv := newTestKV(t)
	store := newCursorStore(kv)

	// 10 days ago is past the 7-day API cutoff.
	stale := time.Now().UTC().Add(-10 * 24 * time.Hour)
	require.NoError(t, store.Save(context.Background(), stale))

	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(time.Now().UTC(), "")}}}
	p := newMaliciousPackagePoller(fake, store)

	handler, _ := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	require.Len(t, fake.captured, 1)
	got := startFromTime(fake.captured[0])
	require.False(t, got.IsZero(), "start_from should be set after reset")

	// After reset, start_from is approximately now - safeStartFromAge.
	expectedReset := time.Now().UTC().Add(-safeStartFromAge)
	delta := got.Sub(expectedReset)
	if delta < 0 {
		delta = -delta
	}
	assert.Less(t, delta, 10*time.Second,
		"start_from after reset must be ~6 days ago; got %v expected ~%v", got, expectedReset)
}

func TestPoll_RecordsWithoutCreatedAt_FallbackToNow(t *testing.T) {
	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(time.Now().UTC(), "",
		recordSpec{id: "a", name: "pkg-a", version: "1.0", skipCreatedAt: true},
	)}}}

	store := newCursorStore(newTestKV(t))
	p := newMaliciousPackagePoller(fake, store)

	before := time.Now().UTC()
	handler, _ := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))
	after := time.Now().UTC()

	saved, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.True(t, !saved.Before(before) && !saved.After(after.Add(time.Second)),
		"cursor falls back to time.Now() when records have no created_at; got %v", saved)
}

func TestPoll_ZeroRecords_StillSavesCursor(t *testing.T) {
	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(time.Now().UTC(), "")}}}
	store := newCursorStore(newTestKV(t))
	p := newMaliciousPackagePoller(fake, store)

	handler, _ := drainHandler(t)
	require.NoError(t, p.Poll(context.Background(), handler))

	saved, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.False(t, saved.IsZero(), "even with 0 records, cursor file is created so operators can edit it")
}

func TestPoll_GRPCFailure_PropagatesAndKeepsCursor(t *testing.T) {
	kv := newTestKV(t)
	store := newCursorStore(kv)
	original := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Microsecond)
	require.NoError(t, store.Save(context.Background(), original))

	fake := &fakeMalysisClient{queue: []fakeResp{{err: errors.New("grpc unavailable")}}}
	p := newMaliciousPackagePoller(fake, store)

	handler, _ := drainHandler(t)
	err := p.Poll(context.Background(), handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list records")

	// On failure, the cursor must NOT advance — next cycle should retry
	// the same window.
	saved, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.True(t, saved.Equal(original), "cursor must not advance after gRPC error; got %v want %v", saved, original)
}

func TestPoll_CallbackError_StopsAndPropagates(t *testing.T) {
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	fake := &fakeMalysisClient{queue: []fakeResp{{resp: makePage(base, "",
		recordSpec{id: "a", name: "pkg-a", version: "1.0"},
		recordSpec{id: "b", name: "pkg-b", version: "2.0"},
	)}}}

	store := newCursorStore(newTestKV(t))
	p := newMaliciousPackagePoller(fake, store)

	stop := errors.New("callback bailed")
	delivered := 0
	err := p.Poll(context.Background(), func(*malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error {
		delivered++
		return stop
	})

	require.ErrorIs(t, err, stop)
	assert.Equal(t, 1, delivered, "callback error stops delivery immediately on the first record")
}
