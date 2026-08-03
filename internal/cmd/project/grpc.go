package project

import (
	"context"
	"fmt"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/tui"
)

type listScansClient interface {
	ListScans(
		context.Context,
		*controltowerv1.ListScansRequest,
		...grpc.CallOption,
	) (*controltowerv1.ListScansResponse, error)
}

type createProjectScansClient interface {
	CreateProjectScans(
		context.Context,
		*controltowerv1.CreateProjectScansRequest,
		...grpc.CallOption,
	) (*controltowerv1.CreateProjectScansResponse, error)
}

func newCreateProjectScansClient(conn grpc.ClientConnInterface) createProjectScansClient {
	return controltowerv1grpc.NewScanServiceClient(conn)
}

func newListScansClient(conn grpc.ClientConnInterface) listScansClient {
	return controltowerv1grpc.NewScanServiceClient(conn)
}

func listProjectScans(
	ctx context.Context,
	client listScansClient,
	in *listInput,
) (*listResult, error) {
	if err := validateListInput(in); err != nil {
		return nil, err
	}

	req, err := newListRequest(in)
	if err != nil {
		return nil, err
	}
	res, err := client.ListScans(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("project scan list: %w", err)
	}
	return translateListResponse(res)
}

func newListRequest(in *listInput) (*controltowerv1.ListScansRequest, error) {
	status, err := parseScanStatus(in.Status)
	if err != nil {
		return nil, err
	}
	trigger, err := parseScanTrigger(in.Trigger)
	if err != nil {
		return nil, err
	}

	req := &controltowerv1.ListScansRequest{}
	req.SetPagination(newPaginationRequest(in.PageSize, in.PageToken))

	// The server treats an absent filter and an all-default filter differently
	// only in cost, but keeping it absent avoids sending empty selectors.
	filter := &controltowerv1.ListScansRequest_Filter{}
	set := false
	if len(in.Projects) > 0 {
		filter.SetProjects(in.Projects)
		set = true
	}
	if len(in.ProjectVersions) > 0 {
		filter.SetProjectVersions(in.ProjectVersions)
		set = true
	}
	if status != messagescontroltowerv1.ScanStatus_SCAN_STATUS_UNSPECIFIED {
		filter.SetStatus(status)
		set = true
	}
	if trigger != messagescontroltowerv1.ScanTrigger_SCAN_TRIGGER_UNSPECIFIED {
		filter.SetTrigger(trigger)
		set = true
	}
	if set {
		req.SetFilter(filter)
	}
	return req, nil
}

func translateListResponse(res *controltowerv1.ListScansResponse) (*listResult, error) {
	sessions := res.GetScanSessions()
	scans := make([]listedScan, 0, len(sessions))
	for i, info := range sessions {
		session := info.GetScanSession()
		if session == nil {
			return nil, fmt.Errorf("project scan list: invalid response: result %d is missing scan session", i+1)
		}
		sessionID := session.GetScanSessionId().GetSessionId()
		if sessionID == "" {
			return nil, fmt.Errorf("project scan list: invalid response: result %d is missing scan session ID", i+1)
		}

		scan := listedScan{
			ScanSessionID: sessionID,
			Status:        tui.EnumToken(session.GetStatus().String(), scanStatusPrefix),
			Trigger:       tui.EnumToken(session.GetTrigger().String(), scanTriggerPrefix),
		}
		if timestamp := session.GetCreatedAt(); timestamp != nil {
			createdAt := timestamp.AsTime().UTC()
			scan.CreatedAt = &createdAt
		}
		if project := info.GetProject().GetProject(); project != nil {
			scan.ProjectID = project.GetProjectId()
			scan.ProjectName = project.GetName()
		}
		if version := info.GetProject().GetProjectVersion(); version != nil {
			scan.ProjectVersion = version.GetVersion()
		}
		if info.HasVulnerabilities() {
			scan.Vulnerabilities = new(info.GetVulnerabilities())
		}
		if info.HasPolicyViolations() {
			scan.PolicyViolations = new(info.GetPolicyViolations())
		}
		if info.HasSuspiciousPackages() {
			scan.SuspiciousPackages = new(info.GetSuspiciousPackages())
		}
		scans = append(scans, scan)
	}

	return &listResult{
		scans:         scans,
		nextPageToken: res.GetPagination().GetNextPageToken(),
	}, nil
}

// paginate walks a cursor-paginated list RPC until the server stops handing out
// a next page token. fetch receives the current page token and returns the next
// one. Returning an empty token from fetch also stops the walk, which lets a
// caller finish early once it has everything it needs. The repeated-token guard
// keeps a server that cycles tokens from looping forever.
func paginate(
	ctx context.Context,
	label string,
	fetch func(ctx context.Context, pageToken string) (string, error),
) error {
	pageToken := ""
	seen := map[string]struct{}{}
	for {
		next, err := fetch(ctx, pageToken)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if _, repeated := seen[next]; repeated {
			return fmt.Errorf("%s: invalid response: repeated page token", label)
		}
		seen[next] = struct{}{}
		pageToken = next
	}
}

func newPaginationRequest(pageSize uint32, pageToken string) *messagescontroltowerv1.PaginationRequest {
	pagination := &messagescontroltowerv1.PaginationRequest{}
	if pageSize > 0 {
		pagination.SetPageSize(pageSize)
	}
	if pageToken != "" {
		pagination.SetPageToken(pageToken)
	}
	return pagination
}

func createProjectScans(
	ctx context.Context,
	client createProjectScansClient,
	projectIDs []string,
) (*createResult, error) {
	if err := validateProjectIDs(projectIDs); err != nil {
		return nil, err
	}

	res, err := client.CreateProjectScans(ctx, newCreateRequest(projectIDs))
	if err != nil {
		return nil, fmt.Errorf("project scan create: %w", err)
	}
	return translateCreateResponse(res, len(projectIDs))
}

func newCreateRequest(projectIDs []string) *controltowerv1.CreateProjectScansRequest {
	targets := make([]*controltowerv1.CreateProjectScansRequest_ProjectScanTarget, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		target := &controltowerv1.CreateProjectScansRequest_ProjectScanTarget{}
		target.SetProjectId(projectID)
		targets = append(targets, target)
	}
	req := &controltowerv1.CreateProjectScansRequest{}
	req.SetTargets(targets)
	return req
}

func translateCreateResponse(
	res *controltowerv1.CreateProjectScansResponse,
	requestCount int,
) (*createResult, error) {
	projectScans := res.GetProjectScans()
	if len(projectScans) != requestCount {
		return nil, fmt.Errorf(
			"project scan create: invalid response: response count %d does not match request count %d",
			len(projectScans),
			requestCount,
		)
	}

	scans := make([]createdScan, 0, len(projectScans))
	for i, projectScan := range projectScans {
		projectID := projectScan.GetProjectId()
		if projectID == "" {
			return nil, fmt.Errorf("project scan create: invalid response: result %d is missing project ID", i+1)
		}
		session := projectScan.GetScanSession()
		if session == nil {
			return nil, fmt.Errorf("project scan create: invalid response: project %q is missing scan session", projectID)
		}
		sessionID := session.GetScanSessionId().GetSessionId()
		if sessionID == "" {
			return nil, fmt.Errorf("project scan create: invalid response: project %q is missing scan session ID", projectID)
		}

		scan := createdScan{
			ProjectID:     projectID,
			ScanSessionID: sessionID,
			Status:        tui.EnumToken(session.GetStatus().String(), "SCAN_STATUS_"),
		}
		if timestamp := session.GetCreatedAt(); timestamp != nil {
			createdAt := timestamp.AsTime().UTC()
			scan.CreatedAt = &createdAt
		}
		scans = append(scans, scan)
	}
	return &createResult{scans: scans}, nil
}
