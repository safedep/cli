package auth

import (
	"fmt"
	"net"
	"net/http"

	drygrpc "github.com/safedep/dry/adapters/grpc"
	"google.golang.org/grpc"
)

const (
	defaultControlPlaneAddr = "cloud.safedep.io:443"
	envControlPlaneAddr     = "SAFEDEP_CLOUD_CONTROL_ADDR"
	tenantIDHeader          = "x-tenant-id"
)

// ControlPlaneConn opens a gRPC connection to the SafeDep control plane
// using the supplied access token. When tenant is empty (e.g. the
// post-OAuth bootstrap call to GetUserInfo before a tenant is known) no
// tenant header is sent. Otherwise the tenant header is set so the
// service can scope responses correctly.
//
// dry/cloud.NewControlPlaneClient requires a non-empty tenant in the
// Credentials struct. This helper sidesteps that constraint for the
// bootstrap step.
func ControlPlaneConn(token, tenant string) (*grpc.ClientConn, error) {
	if token == "" {
		return nil, fmt.Errorf("auth: empty access token")
	}

	host, port := splitAddr(envOr(envControlPlaneAddr, defaultControlPlaneAddr))

	headers := http.Header{}
	if tenant != "" {
		headers.Set(tenantIDHeader, tenant)
	}

	conn, err := drygrpc.GrpcClient(GRPCAppName, host, port, token, headers, []grpc.DialOption{})
	if err != nil {
		return nil, fmt.Errorf("auth: control plane connect: %w", err)
	}
	return conn, nil
}

func splitAddr(addr string) (host, port string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, "443"
	}
	return host, port
}

