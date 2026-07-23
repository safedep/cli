package packages

import (
	"context"
	"fmt"
	"strings"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	paginationv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	malysismsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/malysis/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
	"google.golang.org/grpc"
)

// Four small interfaces, one per RPC, so commands and tests depend on the
// narrowest surface they need. Service is the single gRPC-backed impl.

type ScanSubmitter interface {
	Submit(ctx context.Context, in SubmitInput) (*SubmitResult, error)
}

type ScanGetter interface {
	Get(ctx context.Context, scanID string) (*Scan, error)
}

type ScanLister interface {
	List(ctx context.Context, in ListInput) (*ListResult, error)
}

type ScanReportGetter interface {
	GetReport(ctx context.Context, scanID string) (*Report, error)
}

// Terminal scan status tokens (see statusToken).
const (
	statusCompleted = "completed"
	statusFailed    = "failed"
)

type Service struct {
	client malysisv1grpc.PackageScanServiceClient
}

func NewService(conn *grpc.ClientConn) *Service {
	return &Service{client: malysisv1grpc.NewPackageScanServiceClient(conn)}
}

var (
	_ ScanSubmitter    = (*Service)(nil)
	_ ScanGetter       = (*Service)(nil)
	_ ScanLister       = (*Service)(nil)
	_ ScanReportGetter = (*Service)(nil)
)

// CLI-side types. Proto types are translated at this boundary and never
// leak into command rendering code.

type SubmitInput struct {
	Target         *packagev1.PackageVersion
	IdempotencyKey string
}

type SubmitResult struct {
	ScanID string
	Status string
}

type ListInput struct {
	// Target filters the listing to one package version. Nil means no filter.
	Target    *packagev1.PackageVersion
	PageSize  uint32
	PageToken string
}

type ListResult struct {
	Scans    []Scan
	NextPage string
}

// Scan is the headline record shared by get, list and the run result. It
// carries no report; verdict is empty until the scan is completed.
type Scan struct {
	ScanID      string
	Ecosystem   string
	Name        string
	Version     string
	Status      string
	Verdict     string
	Confidence  float64
	Failure     string
	CreatedAt   time.Time
	CompletedAt time.Time
}

// Report is the full analysis of a completed scan. It embeds the headline
// so renderers get identity and verdict alongside the evidence.
type Report struct {
	Scan
	ReportID         string
	AnalyzedAt       time.Time
	Summary          string
	Details          string
	IsMalware        bool
	FileEvidences    []FileEvidence
	ProjectEvidences []ProjectEvidence
	Warnings         []string
}

type FileEvidence struct {
	File       string
	Line       int32
	Title      string
	Behavior   string
	Details    string
	Confidence string
}

type ProjectEvidence struct {
	Project    string
	URL        string
	Title      string
	Behavior   string
	Details    string
	Confidence string
}

func (s *Service) Submit(ctx context.Context, in SubmitInput) (*SubmitResult, error) {
	target := &malysismsgv1.PackageAnalysisTarget{}
	target.SetPackageVersion(in.Target)

	req := &malysisv1.SubmitScanRequest{}
	req.SetTarget(target)
	if in.IdempotencyKey != "" {
		req.SetIdempotencyKey(in.IdempotencyKey)
	}

	res, err := s.client.SubmitScan(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("package scan: submit: %w", err)
	}
	return &SubmitResult{
		ScanID: res.GetScanId(),
		Status: statusToken(res.GetStatus()),
	}, nil
}

func (s *Service) Get(ctx context.Context, scanID string) (*Scan, error) {
	req := &malysisv1.GetScanRequest{}
	req.SetScanId(scanID)

	res, err := s.client.GetScan(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("package scan: get %s: %w", scanID, err)
	}
	scan := scanFromProto(res.GetScan())
	return &scan, nil
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	req := &malysisv1.ListScansRequest{}
	if in.PageSize > 0 || in.PageToken != "" {
		p := &paginationv1.PaginationRequest{}
		if in.PageSize > 0 {
			p.SetPageSize(in.PageSize)
		}
		if in.PageToken != "" {
			p.SetPageToken(in.PageToken)
		}
		req.SetPagination(p)
	}
	if in.Target != nil {
		target := &malysismsgv1.PackageAnalysisTarget{}
		target.SetPackageVersion(in.Target)
		f := &malysisv1.ListScansRequest_Filters{}
		f.SetTarget(target)
		req.SetFilters(f)
	}

	res, err := s.client.ListScans(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("package scan: list: %w", err)
	}
	out := &ListResult{NextPage: res.GetPagination().GetNextPageToken()}
	for _, ps := range res.GetScans() {
		out.Scans = append(out.Scans, scanFromProto(ps))
	}
	return out, nil
}

func (s *Service) GetReport(ctx context.Context, scanID string) (*Report, error) {
	req := &malysisv1.GetScanReportRequest{}
	req.SetScanId(scanID)

	res, err := s.client.GetScanReport(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("package scan: report %s: %w", scanID, err)
	}
	return reportFromProto(res.GetScan(), res.GetReport()), nil
}

func scanFromProto(ps *malysisv1.PackageScan) Scan {
	pv := ps.GetTarget().GetPackageVersion()
	pkg := pv.GetPackage()
	scan := Scan{
		ScanID:     ps.GetScanId(),
		Ecosystem:  ecosystemToken(pkg.GetEcosystem()),
		Name:       pkg.GetName(),
		Version:    pv.GetVersion(),
		Status:     statusToken(ps.GetStatus()),
		Confidence: ps.GetConfidence(),
		Failure:    ps.GetFailureReason(),
	}
	// Verdict is meaningful only once the scan has completed.
	if ps.GetStatus() == malysisv1.AnalysisStatus_ANALYSIS_STATUS_COMPLETED {
		scan.Verdict = verdictToken(ps.GetVerdict())
	}
	if ps.HasCreatedAt() {
		scan.CreatedAt = ps.GetCreatedAt().AsTime()
	}
	if ps.HasCompletedAt() {
		scan.CompletedAt = ps.GetCompletedAt().AsTime()
	}
	return scan
}

func reportFromProto(ps *malysisv1.PackageScan, r *malysismsgv1.Report) *Report {
	out := &Report{
		Scan:      scanFromProto(ps),
		ReportID:  r.GetReportId(),
		IsMalware: r.GetInference().GetIsMalware(),
		Summary:   r.GetInference().GetSummary(),
		Details:   r.GetInference().GetDetails(),
	}
	if r.GetAnalyzedAt() != nil {
		out.AnalyzedAt = r.GetAnalyzedAt().AsTime()
	}
	for _, fe := range r.GetFileEvidences() {
		ev := fe.GetEvidence()
		out.FileEvidences = append(out.FileEvidences, FileEvidence{
			File:       fe.GetFileKey(),
			Line:       fe.GetLine(),
			Title:      ev.GetTitle(),
			Behavior:   ev.GetBehavior(),
			Details:    ev.GetDetails(),
			Confidence: confidenceToken(ev.GetConfidence()),
		})
	}
	for _, pe := range r.GetProjectEvidences() {
		ev := pe.GetEvidence()
		out.ProjectEvidences = append(out.ProjectEvidences, ProjectEvidence{
			Project:    pe.GetProject().GetName(),
			URL:        pe.GetProject().GetUrl(),
			Title:      ev.GetTitle(),
			Behavior:   ev.GetBehavior(),
			Details:    ev.GetDetails(),
			Confidence: confidenceToken(ev.GetConfidence()),
		})
	}
	for _, w := range r.GetWarnings() {
		out.Warnings = append(out.Warnings, w.GetMessage())
	}
	return out
}

// Enum -> display token helpers. All are generic prefix-trims so new enum
// values render without code changes.

func statusToken(s malysisv1.AnalysisStatus) string {
	t := prettyEnum(s.String(), "ANALYSIS_STATUS_")
	return strings.ReplaceAll(t, "_", "-")
}

func verdictToken(v malysisv1.AnalysisVerdict) string {
	return prettyEnum(v.String(), "ANALYSIS_VERDICT_")
}

func ecosystemToken(e packagev1.Ecosystem) string {
	return prettyEnum(e.String(), "ECOSYSTEM_")
}

func confidenceToken(c malysismsgv1.Report_Evidence_Confidence) string {
	t := prettyEnum(c.String(), "CONFIDENCE_")
	if t == "unknown" {
		return ""
	}
	return t
}

func prettyEnum(name, prefix string) string {
	s := strings.TrimPrefix(name, prefix)
	if s == "" || s == "UNSPECIFIED" {
		return "unknown"
	}
	return strings.ToLower(s)
}
