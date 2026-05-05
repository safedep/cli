package auth

import (
	"errors"
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
	defaultGRPCPort         = "443"
)

// controlPlaneConn is the default ControlPlaneConnFunc used by bootstrap
// when the caller does not inject one. Lives here (not on App) because
// the post-OAuth bootstrap is the only consumer that needs an
// untenanted control-plane connection. dry/cloud.NewControlPlaneClient
// requires a non-empty tenant in the credential, which is unavailable
// before GetUserInfo runs.
func controlPlaneConn(token, tenant string) (*grpc.ClientConn, error) {
	if token == "" {
		return nil, errors.New("auth: control plane conn: empty access token")
	}

	host, port := splitAddr(envOr(envControlPlaneAddr, defaultControlPlaneAddr))

	headers := http.Header{}
	if tenant != "" {
		headers.Set(tenantIDHeader, tenant)
	}

	conn, err := drygrpc.GrpcClient(GRPCAppName, host, port, token, headers, []grpc.DialOption{})
	if err != nil {
		return nil, fmt.Errorf("auth: control plane conn: %w", err)
	}
	return conn, nil
}

func splitAddr(addr string) (host, port string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, defaultGRPCPort
	}
	return host, port
}
