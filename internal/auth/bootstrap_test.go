package auth

import (
	"context"
	"errors"
	"net"
	"testing"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// fakeUserServer is a configurable UserServiceServer that returns a
// predetermined list of tenant domains, cycling through successive calls.
type fakeUserServer struct {
	controltowerv1grpc.UnimplementedUserServiceServer
	// responses returns tenant-domain slices per successive call.
	// When all responses are consumed, the last one is repeated.
	responses [][]string
	callIdx   int
}

func (f *fakeUserServer) GetUserInfo(_ context.Context, _ *controltowerv1.GetUserInfoRequest) (*controltowerv1.GetUserInfoResponse, error) {
	idx := f.callIdx
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	f.callIdx++

	domains := f.responses[idx]
	accesses := make([]*messagescontroltowerv1.Access, 0, len(domains))
	for _, d := range domains {
		accesses = append(accesses, &messagescontroltowerv1.Access{
			Tenant: &messagescontroltowerv1.Tenant{Domain: d},
		})
	}
	return &controltowerv1.GetUserInfoResponse{Access: accesses}, nil
}

// bootstrapTestServer hosts both UserService and OnboardingService on the
// same in-memory listener so PostOAuthBootstrap (which calls both) can be
// tested end-to-end without a real network.
type bootstrapTestServer struct {
	user      *fakeUserServer
	onboarding *fakeOnboardingServer
}

func newBootstrapTestServer(t *testing.T, bts bootstrapTestServer) ControlPlaneConnFunc {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	controltowerv1grpc.RegisterUserServiceServer(srv, bts.user)
	controltowerv1grpc.RegisterOnboardingServiceServer(srv, bts.onboarding)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := lis.Close(); err != nil {
			t.Logf("bufconn close: %v", err)
		}
	})
	return func(token, tenant string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			"passthrough://bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
}

func TestPostOAuthBootstrap_ZeroTenants_NilPrompter_ReturnsExistingError(t *testing.T) {
	connFor := newBootstrapTestServer(t, bootstrapTestServer{
		user: &fakeUserServer{
			responses: [][]string{{}},
		},
		onboarding: &fakeOnboardingServer{},
	})

	_, err := PostOAuthBootstrap(context.Background(), BootstrapInput{
		AccessToken:          "token-123",
		RegistrationPrompter: nil,
		ConnFor:              connFor,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no accessible tenant")
	assert.Contains(t, err.Error(), "contact SafeDep support")
}

func TestPostOAuthBootstrap_ZeroTenants_PrompterSucceeds_TenantReturnedAfterRegister(t *testing.T) {
	connFor := newBootstrapTestServer(t, bootstrapTestServer{
		user: &fakeUserServer{
			// First call: zero tenants. Second call (after registration): one tenant.
			responses: [][]string{
				{},
				{"acme-corp-amber-beacon-abc"},
			},
		},
		onboarding: &fakeOnboardingServer{returnDomain: "acme-corp-amber-beacon-abc"},
	})

	prompter := func() (*RegistrationInput, error) {
		return &RegistrationInput{
			Name:               "Alice",
			OrganizationName:   "Acme Corp",
			OrganizationDomain: "acme-corp-amber-beacon-abc",
		}, nil
	}

	result, err := PostOAuthBootstrap(context.Background(), BootstrapInput{
		AccessToken:          "token-123",
		RegistrationPrompter: prompter,
		ConnFor:              connFor,
	})

	require.NoError(t, err)
	assert.Equal(t, "acme-corp-amber-beacon-abc", result.Tenant)
}

func TestPostOAuthBootstrap_ZeroTenants_PrompterSucceeds_StillZeroAfterRegister_ReturnsError(t *testing.T) {
	connFor := newBootstrapTestServer(t, bootstrapTestServer{
		user: &fakeUserServer{
			// Both calls return zero tenants.
			responses: [][]string{{}, {}},
		},
		onboarding: &fakeOnboardingServer{returnDomain: "acme-corp-amber-beacon-abc"},
	})

	prompter := func() (*RegistrationInput, error) {
		return &RegistrationInput{
			Name:               "Alice",
			OrganizationName:   "Acme Corp",
			OrganizationDomain: "acme-corp-amber-beacon-abc",
		}, nil
	}

	_, err := PostOAuthBootstrap(context.Background(), BootstrapInput{
		AccessToken:          "token-123",
		RegistrationPrompter: prompter,
		ConnFor:              connFor,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "registration succeeded but no tenant found")
	assert.Contains(t, err.Error(), "contact SafeDep support")
}

func TestPostOAuthBootstrap_ZeroTenants_PrompterReturnsError_PropagatesError(t *testing.T) {
	connFor := newBootstrapTestServer(t, bootstrapTestServer{
		user: &fakeUserServer{
			responses: [][]string{{}},
		},
		onboarding: &fakeOnboardingServer{},
	})

	promptErr := errors.New("--no-api-key cannot be used during initial registration: an API key is required to complete setup")
	prompter := func() (*RegistrationInput, error) {
		return nil, promptErr
	}

	_, err := PostOAuthBootstrap(context.Background(), BootstrapInput{
		AccessToken:          "token-123",
		RegistrationPrompter: prompter,
		ConnFor:              connFor,
	})

	require.Error(t, err)
	assert.Equal(t, promptErr, err)
}
