package packages

import (
	"testing"

	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"github.com/stretchr/testify/assert"
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
