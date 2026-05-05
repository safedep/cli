package auth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/dry/log"
	"google.golang.org/grpc"
)

// TenantPicker resolves the tenant when the user has access to multiple.
// Invoked only when len(tenants) > 1 and no preferred tenant matches.
type TenantPicker func(tenants []string) (string, error)

// ControlPlaneConnFunc opens a control-plane gRPC connection for the
// supplied (token, tenant). tenant may be empty for the bootstrap call
// to GetUserInfo. Tests inject a fake here. Production callers leave it
// nil and the package-local default is used.
type ControlPlaneConnFunc func(token, tenant string) (*grpc.ClientConn, error)

// BootstrapInput captures everything PostOAuthBootstrap needs to provision
// a tenant and (optionally) an API key on top of a fresh access token.
type BootstrapInput struct {
	AccessToken     string
	PreferredTenant string

	CreateAPIKey     bool
	APIKeyExpiryDays int
	APIKeyName       string
	Picker           TenantPicker

	// ConnFor is the control-plane connection builder. Optional. When
	// nil, the package-local default is used. Tests inject a fake.
	ConnFor ControlPlaneConnFunc
}

// BootstrapResult reports what the bootstrap step accomplished.
type BootstrapResult struct {
	Tenant          string
	APIKey          string
	APIKeyExpiresAt time.Time
}

// PostOAuthBootstrap completes the work that follows a successful device
// flow: discover accessible tenants, pick one, optionally create an API
// key. It does not write to the keychain. The caller does that, since
// the keychain store is owned by App.
func PostOAuthBootstrap(ctx context.Context, in BootstrapInput) (*BootstrapResult, error) {
	if in.AccessToken == "" {
		return nil, errors.New("auth: bootstrap: empty access token")
	}
	if in.ConnFor == nil {
		in.ConnFor = controlPlaneConn
	}

	tenants, err := listAccessibleTenants(ctx, in.ConnFor, in.AccessToken)
	if err != nil {
		return nil, err
	}
	if len(tenants) == 0 {
		return nil, errors.New("auth: this account has no accessible tenant: contact SafeDep support")
	}

	tenant, err := pickTenant(tenants, in.PreferredTenant, in.Picker)
	if err != nil {
		return nil, err
	}

	res := &BootstrapResult{Tenant: tenant}
	if !in.CreateAPIKey {
		return res, nil
	}

	key, expiresAt, err := createAPIKey(ctx, in.ConnFor, in.AccessToken, tenant, in.APIKeyName, in.APIKeyExpiryDays)
	if err != nil {
		return nil, err
	}
	res.APIKey = key
	res.APIKeyExpiresAt = expiresAt
	return res, nil
}

func listAccessibleTenants(ctx context.Context, connFor ControlPlaneConnFunc, token string) ([]string, error) {
	conn, err := connFor(token, "")
	if err != nil {
		return nil, err
	}
	defer closeConn("user info", conn)

	svc := controltowerv1grpc.NewUserServiceClient(conn)
	resp, err := svc.GetUserInfo(ctx, &controltowerv1.GetUserInfoRequest{})
	if err != nil {
		return nil, fmt.Errorf("auth: get user info: %w", err)
	}

	out := make([]string, 0, len(resp.GetAccess()))
	for _, a := range resp.GetAccess() {
		if d := a.GetTenant().GetDomain(); d != "" {
			out = append(out, d)
		}
	}
	return out, nil
}

func pickTenant(tenants []string, preferred string, picker TenantPicker) (string, error) {
	if preferred != "" {
		for _, t := range tenants {
			if t == preferred {
				return t, nil
			}
		}
	}
	if len(tenants) == 1 {
		return tenants[0], nil
	}
	if picker == nil {
		return "", errors.New("auth: multiple tenants accessible and no picker provided")
	}
	return picker(tenants)
}

func createAPIKey(ctx context.Context, connFor ControlPlaneConnFunc, token, tenant, name string, expiryDays int) (string, time.Time, error) {
	if expiryDays <= 0 || expiryDays > math.MaxInt32 {
		return "", time.Time{}, fmt.Errorf("auth: api key expiry days out of range: %d", expiryDays)
	}

	conn, err := connFor(token, tenant)
	if err != nil {
		return "", time.Time{}, err
	}
	defer closeConn("create api key", conn)

	svc := controltowerv1grpc.NewApiKeyServiceClient(conn)
	desc := "Created by safedep-cli"
	resp, err := svc.CreateApiKey(ctx, &controltowerv1.CreateApiKeyRequest{
		Name:        name,
		Description: &desc,
		ExpiryDays:  int32(expiryDays), // #nosec G115 -- bounds-checked above
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: create api key: %w", err)
	}
	return resp.GetKey(), resp.GetExpiresAt().AsTime(), nil
}

func closeConn(label string, conn *grpc.ClientConn) {
	if err := conn.Close(); err != nil {
		log.Warnf("auth: close %s connection: %v", label, err)
	}
}
