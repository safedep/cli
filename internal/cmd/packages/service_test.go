package packages

import (
	"testing"

	malysismsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/malysis/v1"
	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanFromProto_FailureCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code malysisv1.AnalysisFailureReason
		want string
	}{
		{"unspecified maps to empty", malysisv1.AnalysisFailureReason_ANALYSIS_FAILURE_REASON_UNSPECIFIED, ""},
		{"package not found", malysisv1.AnalysisFailureReason_ANALYSIS_FAILURE_REASON_PACKAGE_NOT_FOUND, failurePackageNotFound},
		{"unknown value passes through", malysisv1.AnalysisFailureReason(99), "99"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ps := &malysisv1.PackageScan{}
			ps.SetStatus(malysisv1.AnalysisStatus_ANALYSIS_STATUS_FAILED)
			ps.SetFailureCode(tc.code)
			assert.Equal(t, tc.want, scanFromProto(ps).FailureCode)
		})
	}
}

func TestReportFromProto_VerdictPrefersInference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		scanVerdict malysisv1.AnalysisVerdict
		inference   malysismsgv1.Report_Inference_Verdict
		wantVerdict string
	}{
		{
			"inconclusive inference overrides benign scan verdict",
			malysisv1.AnalysisVerdict_ANALYSIS_VERDICT_BENIGN,
			malysismsgv1.Report_Inference_VERDICT_INCONCLUSIVE,
			verdictInconclusive,
		},
		{
			"malicious inference maps to malware token",
			malysisv1.AnalysisVerdict_ANALYSIS_VERDICT_UNSPECIFIED,
			malysismsgv1.Report_Inference_VERDICT_MALICIOUS,
			verdictMalware,
		},
		{
			"unspecified inference keeps scan verdict",
			malysisv1.AnalysisVerdict_ANALYSIS_VERDICT_MALWARE,
			malysismsgv1.Report_Inference_VERDICT_UNSPECIFIED,
			verdictMalware,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ps := &malysisv1.PackageScan{}
			ps.SetStatus(malysisv1.AnalysisStatus_ANALYSIS_STATUS_COMPLETED)
			ps.SetVerdict(tc.scanVerdict)

			inf := &malysismsgv1.Report_Inference{}
			inf.SetVerdict(tc.inference)
			r := &malysismsgv1.Report{}
			r.SetInference(inf)

			assert.Equal(t, tc.wantVerdict, reportFromProto(ps, r).Verdict)
		})
	}
}

func TestReportFromProto_Indicators(t *testing.T) {
	t.Parallel()
	c2 := &threatintelv1.IndicatorOfCompromise{}
	c2.SetType(threatintelv1.IndicatorType_INDICATOR_TYPE_C2_DOMAIN)
	c2.SetValue("evil.example.com")
	c2.SetNote("beacon host")
	hash := &threatintelv1.IndicatorOfCompromise{}
	hash.SetType(threatintelv1.IndicatorType_INDICATOR_TYPE_FILE_SHA256)
	hash.SetValue("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	r := &malysismsgv1.Report{}
	r.SetIocs([]*threatintelv1.IndicatorOfCompromise{c2, hash})

	got := reportFromProto(&malysisv1.PackageScan{}, r).Indicators
	require.Len(t, got, 2)
	assert.Equal(t, Indicator{Type: "c2-domain", Value: "evil.example.com", Note: "beacon host"}, got[0])
	assert.Equal(t, "file-sha256", got[1].Type)
}

func TestFailedScanError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		scan Scan
		want string
	}{
		{
			"package not found is actionable",
			Scan{ScanID: "scn_1", Ecosystem: "npm", Name: "left-pad", Version: "9.9.9", Status: statusFailed, Failure: "artifact not found in registry", FailureCode: failurePackageNotFound},
			"scan scn_1 failed: package not found in registry: check that npm left-pad@9.9.9 exists",
		},
		{
			"unknown code falls back to reason",
			Scan{ScanID: "scn_2", Status: statusFailed, Failure: "registry fetch error", FailureCode: "registry-unavailable"},
			"scan scn_2 failed: registry fetch error",
		},
		{
			"no reason provided",
			Scan{ScanID: "scn_3", Status: statusFailed},
			"scan scn_3 failed: no reason provided",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.EqualError(t, failedScanError(tc.scan), tc.want)
		})
	}
}
