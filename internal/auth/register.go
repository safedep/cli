package auth

import (
	"context"
	"fmt"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fallbackOnboardingEmail is used when the caller does not supply an email.
// The server overrides it from the JWT header in production; proto validation
// requires a non-empty value in local and test environments.
const fallbackOnboardingEmail = "cloud@safedep.io"

// RegistrationInput holds user-supplied data for first-time tenant registration.
// It is passed via BootstrapInput.RegistrationPrompter when the user has no
// accessible tenant yet.
type RegistrationInput struct {
	Email              string
	Name               string
	OrganizationName   string
	OrganizationDomain string
}

// RegisterTenantInput holds the parameters for RegisterTenant.
type RegisterTenantInput struct {
	AccessToken        string
	Email              string
	Name               string
	OrganizationName   string
	OrganizationDomain string
	// ConnFor is the control-plane connection builder. Optional. When nil,
	// the package-local default is used. Tests inject a fake.
	ConnFor ControlPlaneConnFunc
}

// RegisterTenant creates a new tenant for a first-time user by calling
// OnboardingService.OnboardUser. It retries up to 3 total attempts on domain
// uniqueness conflicts, regenerating the domain suffix each attempt.
// On exhausted retries it returns a user-facing error message.
func RegisterTenant(ctx context.Context, in RegisterTenantInput) (string, error) {
	if in.ConnFor == nil {
		in.ConnFor = controlPlaneConn
	}

	const maxAttempts = 3
	for range maxAttempts {
		domain, err := attemptOnboardUser(ctx, in)
		if err == nil {
			return domain, nil
		}

		if !isDomainUniquenessError(err) {
			return "", fmt.Errorf("auth: register: %w", err)
		}

		// Regenerate domain for the next attempt.
		in.OrganizationDomain = GenerateTenantDomain(in.OrganizationName)
	}

	return "", fmt.Errorf("auth: register: could not find an available domain after %d attempts: try a different organization name", maxAttempts)
}

func attemptOnboardUser(ctx context.Context, in RegisterTenantInput) (string, error) {
	conn, err := in.ConnFor(in.AccessToken, "")
	if err != nil {
		return "", err
	}
	defer closeConn("register tenant", conn)

	email := in.Email
	if email == "" {
		email = fallbackOnboardingEmail
	}

	svc := controltowerv1grpc.NewOnboardingServiceClient(conn)
	resp, err := svc.OnboardUser(ctx, &controltowerv1.OnboardUserRequest{
		Email:              email,
		Name:               in.Name,
		OrganizationName:   in.OrganizationName,
		OrganizationDomain: in.OrganizationDomain,
	})
	if err != nil {
		return "", err
	}

	return resp.GetTenant().GetDomain(), nil
}

func isDomainUniquenessError(err error) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.AlreadyExists
}
