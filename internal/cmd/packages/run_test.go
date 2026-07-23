package packages

import (
	"context"
	"testing"
	"time"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTarget(t *testing.T) *packagev1.PackageVersion {
	t.Helper()
	pv, err := resolveExplicit(targetFlags{Ecosystem: "npm", Name: "lodash", Version: "4.17.21"})
	require.NoError(t, err)
	return pv
}

func TestRunScan_NoWait(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		submitFn: func(_ context.Context, _ SubmitInput) (*SubmitResult, error) {
			return &SubmitResult{ScanID: "scn_1", Status: "queued"}, nil
		},
	}
	res, err := runScan(context.Background(), svc, runInput{Target: testTarget(t), Wait: false}, nil)
	require.NoError(t, err)
	assert.True(t, res.submitted)
	assert.Equal(t, "scn_1", res.scan.ScanID)
	assert.Equal(t, "queued", res.scan.Status)
	assert.Equal(t, "npm", res.scan.Ecosystem)
	assert.Nil(t, res.report)
}

func TestRunScan_DefaultIdempotencyKey(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		submitFn: func(_ context.Context, _ SubmitInput) (*SubmitResult, error) {
			return &SubmitResult{ScanID: "scn_1", Status: "queued"}, nil
		},
	}
	_, err := runScan(context.Background(), svc, runInput{Target: testTarget(t), Wait: false}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, svc.gotSubmit.IdempotencyKey, "default run derives a dedup key")
}

func TestRunScan_RescanClearsKey(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		submitFn: func(_ context.Context, _ SubmitInput) (*SubmitResult, error) {
			return &SubmitResult{ScanID: "scn_1", Status: "queued"}, nil
		},
	}
	_, err := runScan(context.Background(), svc, runInput{Target: testTarget(t), Wait: false, Rescan: true}, nil)
	require.NoError(t, err)
	assert.Empty(t, svc.gotSubmit.IdempotencyKey, "--rescan forces a fresh scan")
}

func TestRunScan_WaitPollsToTerminal(t *testing.T) {
	t.Parallel()
	statuses := []string{"queued", "in-progress", "completed"}
	svc := &fakeService{
		submitFn: func(_ context.Context, _ SubmitInput) (*SubmitResult, error) {
			return &SubmitResult{ScanID: "scn_1", Status: "queued"}, nil
		},
		getFn: func(_ context.Context, _ string) (*Scan, error) {
			s := statuses[0]
			if len(statuses) > 1 {
				statuses = statuses[1:]
			}
			return &Scan{ScanID: "scn_1", Status: s, Verdict: verdictBenign, Confidence: 0.98}, nil
		},
	}
	var seen []string
	res, err := runScan(context.Background(), svc, runInput{Target: testTarget(t), Wait: true, Timeout: time.Minute},
		func(status string) { seen = append(seen, status) })
	require.NoError(t, err)
	assert.Equal(t, "completed", res.scan.Status)
	assert.Equal(t, verdictBenign, res.scan.Verdict)
	assert.Nil(t, res.report, "benign verdict does not fetch the report")
	assert.Equal(t, []string{"queued", "in-progress", "completed"}, seen)
}

func TestRunScan_MalwareFetchesReport(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		submitFn: func(_ context.Context, _ SubmitInput) (*SubmitResult, error) {
			return &SubmitResult{ScanID: "scn_1", Status: "queued"}, nil
		},
		getFn: func(_ context.Context, _ string) (*Scan, error) {
			return &Scan{ScanID: "scn_1", Status: "completed", Verdict: verdictMalware}, nil
		},
		reportFn: func(_ context.Context, id string) (*Report, error) {
			return &Report{Scan: Scan{ScanID: id}, IsMalware: true, Summary: "bad"}, nil
		},
	}
	res, err := runScan(context.Background(), svc, runInput{Target: testTarget(t), Wait: true, Timeout: time.Minute}, nil)
	require.NoError(t, err)
	require.NotNil(t, res.report)
	assert.True(t, res.report.IsMalware)
}

func TestRunScan_Timeout(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		submitFn: func(_ context.Context, _ SubmitInput) (*SubmitResult, error) {
			return &SubmitResult{ScanID: "scn_1", Status: "queued"}, nil
		},
		getFn: func(_ context.Context, _ string) (*Scan, error) {
			return &Scan{ScanID: "scn_1", Status: "in-progress"}, nil
		},
	}
	// 1ns timeout: the first non-terminal poll trips the deadline before any sleep.
	_, err := runScan(context.Background(), svc, runInput{Target: testTarget(t), Wait: true, Timeout: time.Nanosecond}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestRunResult_RenderTable_MalwareExpandsReport(t *testing.T) {
	t.Parallel()
	res := &runResult{
		scan:   Scan{ScanID: "scn_1", Ecosystem: "npm", Name: "evil", Version: "1.0", Status: "completed", Verdict: verdictMalware},
		report: &Report{IsMalware: true, Summary: "exfiltrates tokens"},
	}
	out := res.RenderTable()
	assert.Contains(t, out, "exfiltrates tokens")
}

func TestRunResult_RenderTable_BenignShowsHint(t *testing.T) {
	t.Parallel()
	res := &runResult{scan: Scan{ScanID: "scn_1", Status: "completed", Verdict: verdictBenign}}
	out := res.RenderTable()
	assert.Contains(t, out, "package scan show")
}

func TestRunResult_RenderJSON_StableRegardlessOfVerdict(t *testing.T) {
	t.Parallel()
	res := &runResult{
		scan:   Scan{ScanID: "scn_1", Ecosystem: "npm", Name: "evil", Version: "1.0", Status: "completed", Verdict: verdictMalware},
		report: &Report{IsMalware: true, Summary: "should not appear in run json"},
	}
	b, err := res.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), "\"verdict\": \"malware\"")
	assert.NotContains(t, string(b), "should not appear")
}
