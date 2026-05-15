package auth

import (
	"context"
	"net"
	"testing"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// fakeOnboardingServer is a configurable OnboardingServiceServer that
// returns queued errors (or nil) in order, then succeeds, recording each
// request's OrganizationDomain for assertions.
type fakeOnboardingServer struct {
	controltowerv1grpc.UnimplementedOnboardingServiceServer
	// errors to return in order; nil entry means success.
	errors         []error
	callIdx        int
	recordedDomain []string
	returnDomain   string
}

func (f *fakeOnboardingServer) OnboardUser(_ context.Context, req *controltowerv1.OnboardUserRequest) (*controltowerv1.OnboardUserResponse, error) {
	f.recordedDomain = append(f.recordedDomain, req.OrganizationDomain)

	idx := f.callIdx
	f.callIdx++

	if idx < len(f.errors) && f.errors[idx] != nil {
		return nil, f.errors[idx]
	}

	d := f.returnDomain
	if d == "" {
		d = req.OrganizationDomain
	}

	return &controltowerv1.OnboardUserResponse{
		Tenant: &messagescontroltowerv1.Tenant{Domain: d},
	}, nil
}

// bufconnConnFor builds a ControlPlaneConnFunc that always dials the given
// in-memory listener, ignoring the token and tenant.
func bufconnConnFor(lis *bufconn.Listener) ControlPlaneConnFunc {
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

func newTestServer(t *testing.T, fake *fakeOnboardingServer) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	controltowerv1grpc.RegisterOnboardingServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := lis.Close(); err != nil {
			t.Logf("bufconn close: %v", err)
		}
	})
	return lis
}

func TestRegisterTenant_SuccessFirstAttempt(t *testing.T) {
	fake := &fakeOnboardingServer{returnDomain: "acme-corp-amber-beacon-abc"}
	lis := newTestServer(t, fake)

	domain, err := RegisterTenant(context.Background(), RegisterTenantInput{
		AccessToken:        "token-123",
		Name:               "Alice",
		OrganizationName:   "Acme Corp",
		OrganizationDomain: "acme-corp-amber-beacon-abc",
		ConnFor:            bufconnConnFor(lis),
	})

	require.NoError(t, err)
	assert.Equal(t, "acme-corp-amber-beacon-abc", domain)
	assert.Equal(t, 1, fake.callIdx, "should have made exactly one call")
}

func TestRegisterTenant_AlreadyExistsFirstAttemptSuccessSecond(t *testing.T) {
	alreadyExists := status.Error(codes.AlreadyExists, "domain already exists")
	fake := &fakeOnboardingServer{
		errors:       []error{alreadyExists},
		returnDomain: "acme-corp-brisk-harbor-xyz",
	}
	lis := newTestServer(t, fake)

	firstDomain := "acme-corp-amber-beacon-abc"
	domain, err := RegisterTenant(context.Background(), RegisterTenantInput{
		AccessToken:        "token-123",
		Name:               "Alice",
		OrganizationName:   "Acme Corp",
		OrganizationDomain: firstDomain,
		ConnFor:            bufconnConnFor(lis),
	})

	require.NoError(t, err)
	assert.Equal(t, 2, fake.callIdx, "should have made two calls")
	// The domain sent on the second attempt must differ from the first.
	require.Len(t, fake.recordedDomain, 2)
	assert.NotEqual(t, fake.recordedDomain[0], fake.recordedDomain[1],
		"second attempt should use a regenerated domain")
	assert.Equal(t, fake.returnDomain, domain)
}

func TestRegisterTenant_AlreadyExistsThreeTimesReturnsError(t *testing.T) {
	alreadyExists := status.Error(codes.AlreadyExists, "domain already exists")
	fake := &fakeOnboardingServer{
		errors: []error{alreadyExists, alreadyExists, alreadyExists},
	}
	lis := newTestServer(t, fake)

	_, err := RegisterTenant(context.Background(), RegisterTenantInput{
		AccessToken:        "token-123",
		Name:               "Alice",
		OrganizationName:   "Acme Corp",
		OrganizationDomain: "acme-corp-amber-beacon-abc",
		ConnFor:            bufconnConnFor(lis),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 attempts")
	assert.Contains(t, err.Error(), "organization name")
	assert.Equal(t, 3, fake.callIdx, "should have exhausted all 3 attempts")
}

func TestRegisterTenant_NonUniquenessErrorReturnedImmediately(t *testing.T) {
	permissionDenied := status.Error(codes.PermissionDenied, "not authorized")
	fake := &fakeOnboardingServer{
		errors: []error{permissionDenied},
	}
	lis := newTestServer(t, fake)

	_, err := RegisterTenant(context.Background(), RegisterTenantInput{
		AccessToken:        "token-123",
		Name:               "Alice",
		OrganizationName:   "Acme Corp",
		OrganizationDomain: "acme-corp-amber-beacon-abc",
		ConnFor:            bufconnConnFor(lis),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth: register:")
	// Must not retry: only one call.
	assert.Equal(t, 1, fake.callIdx, "should not retry on non-uniqueness error")
}
