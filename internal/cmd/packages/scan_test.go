package packages

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunGet_ByScanID(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		getFn: func(_ context.Context, id string) (*Scan, error) {
			return &Scan{ScanID: id, Status: "completed", Verdict: verdictBenign}, nil
		},
	}
	scan, err := runGet(context.Background(), svc, getInput{ScanID: "scn_9"})
	require.NoError(t, err)
	assert.Equal(t, "scn_9", scan.ScanID)
	assert.Equal(t, 1, svc.getCalls)
}

func TestRunGet_ByPackageRefUsesLatest(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		listFn: func(_ context.Context, in ListInput) (*ListResult, error) {
			require.NotNil(t, in.Target, "package-ref get filters by target")
			return &ListResult{Scans: []Scan{{ScanID: "scn_new", Status: "completed"}}}, nil
		},
	}
	scan, err := runGet(context.Background(), svc, getInput{Ref: "pkg:npm/lodash@4.17.21"})
	require.NoError(t, err)
	assert.Equal(t, "scn_new", scan.ScanID)
	assert.Equal(t, 0, svc.getCalls, "package-ref path reuses the list record, no extra Get")
}

func TestLatestScan_NoneFound(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		listFn: func(_ context.Context, _ ListInput) (*ListResult, error) {
			return &ListResult{}, nil
		},
	}
	_, err := runGet(context.Background(), svc, getInput{Ref: "pkg:npm/lodash@4.17.21"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no scan found")
}

func TestRunList_BuildsTargetFilter(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		listFn: func(_ context.Context, _ ListInput) (*ListResult, error) {
			return &ListResult{Scans: []Scan{{ScanID: "scn_1"}}, NextPage: "tok"}, nil
		},
	}
	res, err := runList(context.Background(), svc, listInput{
		Flags: targetFlags{Ecosystem: "npm", Name: "lodash", Version: "4.17.21"},
	})
	require.NoError(t, err)
	require.NotNil(t, svc.gotList.Target)
	assert.Equal(t, "lodash", svc.gotList.Target.GetPackage().GetName())
	assert.Equal(t, "tok", res.nextPage)
}

func TestRunList_PartialFilterRejected(t *testing.T) {
	t.Parallel()
	svc := &fakeService{listFn: func(_ context.Context, _ ListInput) (*ListResult, error) { return &ListResult{}, nil }}
	_, err := runList(context.Background(), svc, listInput{Flags: targetFlags{Ecosystem: "npm"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "together")
}

func TestListResult_Render(t *testing.T) {
	t.Parallel()
	r := &listResult{scans: []Scan{
		{ScanID: "scn_1", Ecosystem: "npm", Name: "lodash", Version: "4.17.21", Status: "completed", Verdict: verdictBenign},
	}, nextPage: "tok"}

	table := r.RenderTable()
	assert.Contains(t, table, "lodash")
	assert.Contains(t, table, "--page-token tok")

	plain := r.RenderPlain()
	assert.Contains(t, plain, "lodash")

	js, err := r.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js), "\"next_page_token\": \"tok\"")
}

func TestRunShow_ByScanID(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		reportFn: func(_ context.Context, id string) (*Report, error) {
			return &Report{Scan: Scan{ScanID: id, Status: "completed", Verdict: verdictBenign}, Summary: "clean"}, nil
		},
	}
	res, err := runShow(context.Background(), svc, showInput{ScanID: "scn_5"})
	require.NoError(t, err)
	assert.Equal(t, "scn_5", res.report.ScanID)
}

func TestRunShow_NotCompleted(t *testing.T) {
	t.Parallel()
	svc := &fakeService{
		reportFn: func(_ context.Context, id string) (*Report, error) {
			return &Report{Scan: Scan{ScanID: id, Status: "in-progress"}}, nil
		},
	}
	_, err := runShow(context.Background(), svc, showInput{ScanID: "scn_5"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no report yet")
}

func TestShowResult_TableCapsFileEvidenceButJSONKeepsAll(t *testing.T) {
	t.Parallel()
	files := make([]FileEvidence, maxEvidenceRows+15)
	for i := range files {
		files[i] = FileEvidence{File: fmt.Sprintf("src/file_%d.js", i), Title: "signal"}
	}
	res := &showResult{report: &Report{
		Scan:          Scan{ScanID: "scn_1", Ecosystem: "npm", Name: "big", Version: "1.0", Status: "completed", Verdict: verdictMalware},
		IsMalware:     true,
		FileEvidences: files,
	}}

	table := res.RenderTable()
	assert.Contains(t, table, fmt.Sprintf("showing %d of %d", maxEvidenceRows, len(files)))
	assert.Contains(t, table, "--output json")
	assert.Contains(t, table, "file_0.js")
	assert.NotContains(t, table, "file_34.js", "rows beyond the cap are omitted from the table")

	js, err := res.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js), "file_34.js", "json keeps the full list")
}

func TestShowResult_RenderSkipsEmptySections(t *testing.T) {
	t.Parallel()
	res := &showResult{report: &Report{
		Scan:    Scan{ScanID: "scn_1", Ecosystem: "pypi", Name: "requests", Version: "2.31.0", Status: "completed", Verdict: verdictBenign},
		Summary: "no malicious behavior detected",
	}}
	out := res.RenderTable()
	assert.Contains(t, out, "no malicious behavior detected")
	assert.NotContains(t, out, "File evidence")
	assert.NotContains(t, out, "Warnings")
}
