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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	// DecideAPIKey runs after the tenant is picked, so that a stored key for
	// another tenant is replaced. When it keeps the stored key, no key is
	// created. Nil means that a key is always created.
	DecideAPIKey func(ctx context.Context, tenant string) APIKeyDecision

	// RegistrationPrompter is called when the user has no accessible tenant.
	// It should collect registration data interactively and return it. When
	// nil, the zero-tenant case returns the existing "no accessible tenant"
	// error (preserving behaviour for callers that do not support registration).
	RegistrationPrompter func() (*RegistrationInput, error)

	// ConnFor is the control-plane connection builder. Optional. When
	// nil, the package-local default is used. Tests inject a fake.
	ConnFor ControlPlaneConnFunc
}

// BootstrapResult reports what the bootstrap step accomplished.
type BootstrapResult struct {
	Tenant          string
	APIKey          string
	APIKeyID        string
	APIKeyExpiresAt time.Time

	// APIKeyDecision is what DecideAPIKey returned. Zero when it did not run.
	APIKeyDecision APIKeyDecision

	// APIKeyError is why login could not create a key when DecideAPIKey ran
	// and no usable key remains. The OAuth tokens of the login still work,
	// so the caller saves them and reports the error as a warning.
	APIKeyError error
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
		if in.RegistrationPrompter == nil {
			return nil, errors.New("auth: this account has no accessible tenant: contact SafeDep support")
		}
		tenants, err = registerAndRefetch(ctx, in)
		if err != nil {
			return nil, err
		}
	}

	tenant, err := pickTenant(tenants, in.PreferredTenant, in.Picker)
	if err != nil {
		return nil, err
	}

	res := &BootstrapResult{Tenant: tenant}
	if !in.CreateAPIKey {
		return res, nil
	}

	if in.DecideAPIKey != nil {
		res.APIKeyDecision = in.DecideAPIKey(ctx, tenant)
		if res.APIKeyDecision.Keep {
			return res, nil
		}
	}

	key, err := createAPIKey(ctx, in.ConnFor, in.AccessToken, tenant, in.APIKeyName, in.APIKeyExpiryDays)
	if err != nil && in.DecideAPIKey != nil {
		// A failed create must not cost the user the OAuth tokens of this
		// login. A Viewer cannot create keys. An explicit --rotate-api-key
		// has no DecideAPIKey and fails instead.
		decision := res.APIKeyDecision
		if decision.Fallback && (decision.ExpiresAt.IsZero() || time.Now().Before(decision.ExpiresAt)) {
			warning := fmt.Sprintf("could not create a new API key, so the stored key stays: %v", err)
			if decision.Warning != "" {
				warning = decision.Warning + ". " + warning
			}
			res.APIKeyDecision = APIKeyDecision{Keep: true, ExpiresAt: decision.ExpiresAt, Warning: warning}
			return res, nil
		}
		res.APIKeyError = err
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	res.APIKey = key.GetKey()
	res.APIKeyID = key.GetKeyId()
	// AsTime on a missing timestamp returns 1970-01-01. Saved as the expiry,
	// it would make every later login replace a valid key.
	if key.GetExpiresAt() != nil {
		res.APIKeyExpiresAt = key.GetExpiresAt().AsTime()
	}
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
		// NotFound means the user has no control-tower account yet. Treat as
		// zero tenants so PostOAuthBootstrap triggers the registration flow.
		if status.Code(err) == codes.NotFound {
			return []string{}, nil
		}
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

func createAPIKey(ctx context.Context, connFor ControlPlaneConnFunc, token, tenant, name string,
	expiryDays int,
) (*controltowerv1.CreateApiKeyResponse, error) {
	if expiryDays <= 0 || expiryDays > math.MaxInt32 {
		return nil, fmt.Errorf("auth: api key expiry days out of range: %d", expiryDays)
	}

	conn, err := connFor(token, tenant)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("auth: create api key: %w", err)
	}
	return resp, nil
}

// registerAndRefetch calls RegistrationPrompter to collect registration data,
// registers the tenant, then re-fetches accessible tenants. It returns an
// error if the prompter fails, if registration fails, or if the re-fetch
// still returns zero tenants.
func registerAndRefetch(ctx context.Context, in BootstrapInput) ([]string, error) {
	reg, err := in.RegistrationPrompter()
	if err != nil {
		return nil, err
	}
	if reg == nil {
		return nil, errors.New("auth: registration prompter returned nil input")
	}

	_, err = RegisterTenant(ctx, RegisterTenantInput{
		AccessToken:        in.AccessToken,
		Email:              reg.Email,
		Name:               reg.Name,
		OrganizationName:   reg.OrganizationName,
		OrganizationDomain: reg.OrganizationDomain,
		ConnFor:            in.ConnFor,
	})
	if err != nil {
		return nil, err
	}

	tenants, err := listAccessibleTenants(ctx, in.ConnFor, in.AccessToken)
	if err != nil {
		return nil, err
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("auth: registration succeeded but no tenant found: contact SafeDep support")
	}
	return tenants, nil
}

func closeConn(label string, conn *grpc.ClientConn) {
	if err := conn.Close(); err != nil {
		log.Warnf("auth: close %s connection: %v", label, err)
	}
}
