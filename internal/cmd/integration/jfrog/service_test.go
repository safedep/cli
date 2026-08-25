package jfrog

import (
	"context"
	"errors"
	"net/http"
	"testing"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeXrayClient records which port method the service called, so routing can
// be asserted without a live JFrog server.
type fakeXrayClient struct {
	pushed  []string
	deleted []string
	pushErr error
	delErr  error
	delStat int
}

func (f *fakeXrayClient) validate(context.Context) error { return nil }

func (f *fakeXrayClient) pushMaliciousPackage(_ context.Context, r *threatintelv1.PackageReport) (string, int, error) {
	f.pushed = append(f.pushed, r.GetReportId())
	if f.pushErr != nil {
		return r.GetReportId(), 0, f.pushErr
	}
	return issueID(r), http.StatusCreated, nil
}

func (f *fakeXrayClient) deleteMaliciousPackage(_ context.Context, r *threatintelv1.PackageReport) (string, int, error) {
	f.deleted = append(f.deleted, r.GetReportId())
	if f.delErr != nil {
		return r.GetReportId(), 0, f.delErr
	}
	stat := f.delStat
	if stat == 0 {
		stat = http.StatusOK
	}
	return issueID(r), stat, nil
}

func TestHandleRecord_RoutesWithdrawnToDeleteElsePush(t *testing.T) {
	push := newTestReport("push-1", "pkg-a", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")

	withdrawn := newTestReport("wd-1", "pkg-b", packagev1.Ecosystem_ECOSYSTEM_NPM, "2.0.0")
	withdrawn.SetWithdrawn(true)

	fake := &fakeXrayClient{}
	svc := newFeedService(nil, fake)

	require.NoError(t, svc.handleRecord(context.Background(), push))
	require.NoError(t, svc.handleRecord(context.Background(), withdrawn))

	assert.Equal(t, []string{"push-1"}, fake.pushed, "a live report is pushed")
	assert.Equal(t, []string{"wd-1"}, fake.deleted, "a withdrawn report is deleted")
}

func TestHandleRecord_DeleteFailureIsNotFatal(t *testing.T) {
	withdrawn := newTestReport("wd-1", "pkg-b", packagev1.Ecosystem_ECOSYSTEM_NPM)
	withdrawn.SetWithdrawn(true)

	fake := &fakeXrayClient{delErr: errors.New("boom")}
	svc := newFeedService(nil, fake)

	// A delete error is logged, never returned: one bad delete cannot stop the
	// daemon.
	assert.NoError(t, svc.handleRecord(context.Background(), withdrawn))
}

func TestHandleRecord_Delete404IsHandled(t *testing.T) {
	withdrawn := newTestReport("wd-1", "pkg-b", packagev1.Ecosystem_ECOSYSTEM_NPM)
	withdrawn.SetWithdrawn(true)

	fake := &fakeXrayClient{delStat: http.StatusNotFound}
	svc := newFeedService(nil, fake)

	assert.NoError(t, svc.handleRecord(context.Background(), withdrawn))
	assert.Equal(t, []string{"wd-1"}, fake.deleted)
}

func TestDisplayVersions(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "empty means all", versions: nil, want: "all"},
		{name: "only empty entries means all", versions: []string{"", ""}, want: "all"},
		{name: "single version", versions: []string{"1.0.0"}, want: "1.0.0"},
		{name: "many versions joined", versions: []string{"9.9.9", "9.9.10"}, want: "9.9.9, 9.9.10"},
		{name: "empty entries dropped", versions: []string{"", "1.0.0", ""}, want: "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, displayVersions(tt.versions))
		})
	}
}
