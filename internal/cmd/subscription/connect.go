package subscription

import (
	"context"
	"net/http"
	"os"

	ctconnect "buf.build/gen/go/safedep/api/connectrpc/go/safedep/services/controltower/v1/controltowerv1connect"
	"connectrpc.com/connect"
	"github.com/safedep/cli/internal/app"
)

// The subscription/billing commands talk to the control plane over the
// ConnectRPC protocol rather than native gRPC. Connect carries RPC errors and
// their rich ErrorDetail in the response body, not in HTTP/2 trailers, which
// sidesteps the intermittent trailer loss observed through the gRPC proxy path
// (the proxy mishandles bodyless, trailer-only error responses).

const (
	envControlPlaneAddr     = "SAFEDEP_CLOUD_CONTROL_ADDR"
	defaultControlPlaneAddr = "cloud.safedep.io:443"
	insecureTransportEnv    = "INSECURE_GRPC_CLIENT_USE_INSECURE_TRANSPORT"
	authorizationHeader     = "authorization"
	tenantIDHeader          = "x-tenant-id"
)

// serviceFor builds a Connect-backed Service from the app's control-plane
// credentials, refreshing the token if needed.
func serviceFor(a *app.App) (*Service, error) {
	token, tenant, err := a.ControlPlaneCredentials()
	if err != nil {
		return nil, err
	}
	return newConnectService(token, tenant), nil
}

func controlPlaneBaseURL() string {
	addr := os.Getenv(envControlPlaneAddr)
	if addr == "" {
		addr = defaultControlPlaneAddr
	}
	scheme := "https"
	if os.Getenv(insecureTransportEnv) == "true" {
		scheme = "http"
	}
	return scheme + "://" + addr
}

// authInterceptor injects the same credentials the gRPC per-RPC creds set:
// the raw token in `authorization` and the tenant domain in `x-tenant-id`.
func authInterceptor(token, tenant string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" {
				req.Header().Set(authorizationHeader, token)
			}
			if tenant != "" {
				req.Header().Set(tenantIDHeader, tenant)
			}
			return next(ctx, req)
		}
	}
}

func newConnectService(token, tenant string) *Service {
	baseURL := controlPlaneBaseURL()
	opts := connect.WithInterceptors(authInterceptor(token, tenant))
	return &Service{
		sub:     ctconnect.NewSubscriptionServiceClient(http.DefaultClient, baseURL, opts),
		billing: ctconnect.NewBillingServiceClient(http.DefaultClient, baseURL, opts),
	}
}
