package scan

import (
	"context"
	"fmt"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc"

	"github.com/safedep/cli/internal/tui"
)

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
		return nil, fmt.Errorf("scan create: %w", err)
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
			"scan create: invalid response: response count %d does not match request count %d",
			len(projectScans),
			requestCount,
		)
	}

	scans := make([]createdScan, 0, len(projectScans))
	for i, projectScan := range projectScans {
		projectID := projectScan.GetProjectId()
		if projectID == "" {
			return nil, fmt.Errorf("scan create: invalid response: result %d is missing project ID", i+1)
		}
		session := projectScan.GetScanSession()
		if session == nil {
			return nil, fmt.Errorf("scan create: invalid response: project %q is missing scan session", projectID)
		}
		sessionID := session.GetScanSessionId().GetSessionId()
		if sessionID == "" {
			return nil, fmt.Errorf("scan create: invalid response: project %q is missing scan session ID", projectID)
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
