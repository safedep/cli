package auth

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

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
	"google.golang.org/protobuf/types/known/timestamppb"
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
	user       *fakeUserServer
	onboarding *fakeOnboardingServer
	apiKeys    *fakeAPIKeyServer
}

// fakeAPIKeyServer creates one API key per call and counts the calls.
type fakeAPIKeyServer struct {
	controltowerv1grpc.UnimplementedApiKeyServiceServer
	creates   int
	expiresAt time.Time
	err       error
}

func (f *fakeAPIKeyServer) CreateApiKey(_ context.Context, req *controltowerv1.CreateApiKeyRequest,
) (*controltowerv1.CreateApiKeyResponse, error) {
	f.creates++
	if f.err != nil {
		return nil, f.err
	}
	resp := &controltowerv1.CreateApiKeyResponse{Key: "sk-new", KeyId: "key-new"}
	if !f.expiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(f.expiresAt)
	}
	return resp, nil
}

func newBootstrapTestServer(t *testing.T, bts bootstrapTestServer) ControlPlaneConnFunc {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	controltowerv1grpc.RegisterUserServiceServer(srv, bts.user)
	controltowerv1grpc.RegisterOnboardingServiceServer(srv, bts.onboarding)
	if bts.apiKeys != nil {
		controltowerv1grpc.RegisterApiKeyServiceServer(srv, bts.apiKeys)
	}
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
			Email:              "alice@example.com",
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
			Email:              "alice@example.com",
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

// The decision on the stored key runs on the tenant that the user picked, so
// a key of the previous tenant is replaced. A kept key means no new key.
func TestPostOAuthBootstrap_DecideAPIKey(t *testing.T) {
	expiresAt := time.Date(2027, 2, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		decision    APIKeyDecision
		noExpiry    bool
		wantCreates int
		wantKey     string
	}{
		{
			name:        "a kept key creates no key",
			decision:    APIKeyDecision{Keep: true, ExpiresAt: expiresAt},
			wantCreates: 0,
		},
		{
			name:        "a replaced key creates a key with its id and expiry",
			decision:    APIKeyDecision{Reason: "the stored API key expired on 2026-11-20"},
			wantCreates: 1,
			wantKey:     "sk-new",
		},
		{
			// A missing expiry must not become 1970-01-01, which every later
			// login would read as expired.
			name:        "a response with no expiry records no expiry",
			decision:    APIKeyDecision{Reason: "no API key is stored"},
			noExpiry:    true,
			wantCreates: 1,
			wantKey:     "sk-new",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			apiKeys := &fakeAPIKeyServer{expiresAt: expiresAt}
			if tc.noExpiry {
				apiKeys.expiresAt = time.Time{}
			}
			connFor := newBootstrapTestServer(t, bootstrapTestServer{
				user:       &fakeUserServer{responses: [][]string{{"old.safedep.io", "new.safedep.io"}}},
				onboarding: &fakeOnboardingServer{},
				apiKeys:    apiKeys,
			})

			var decidedFor string
			res, err := PostOAuthBootstrap(context.Background(), BootstrapInput{
				AccessToken:      "token-123",
				PreferredTenant:  "old.safedep.io-gone",
				CreateAPIKey:     true,
				APIKeyExpiryDays: 90,
				Picker:           func([]string) (string, error) { return "new.safedep.io", nil },
				DecideAPIKey: func(_ context.Context, tenant string) APIKeyDecision {
					decidedFor = tenant
					return tc.decision
				},
				ConnFor: connFor,
			})
			require.NoError(t, err)

			assert.Equal(t, "new.safedep.io", decidedFor, "the decision must see the picked tenant")
			assert.Equal(t, tc.wantCreates, apiKeys.creates)
			assert.Equal(t, tc.decision, res.APIKeyDecision)
			assert.Equal(t, tc.wantKey, res.APIKey)
			if tc.wantKey != "" {
				assert.Equal(t, "key-new", res.APIKeyID)
				assert.True(t, apiKeys.expiresAt.Equal(res.APIKeyExpiresAt),
					"expiry %v, want %v", res.APIKeyExpiresAt, apiKeys.expiresAt)
			}
		})
	}
}

// A login must not lose its OAuth tokens because the create failed while the
// stored key still works, for example for a user who cannot create keys.
func TestPostOAuthBootstrap_CreateFails(t *testing.T) {
	expiresAt := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	keep := func(d APIKeyDecision) func(context.Context, string) APIKeyDecision {
		return func(context.Context, string) APIKeyDecision { return d }
	}

	tests := []struct {
		name string
		// decide is nil for --rotate-api-key.
		decide          func(context.Context, string) APIKeyDecision
		wantErr         bool
		wantKeep        bool
		wantExpiresAt   time.Time
		wantWarnings    []string
		wantCreateError bool
	}{
		{
			name:          "a stored key that still works is kept",
			decide:        keep(APIKeyDecision{Reason: "the stored API key expires soon", Fallback: true, ExpiresAt: expiresAt}),
			wantKeep:      true,
			wantExpiresAt: expiresAt,
			wantWarnings:  []string{"could not create a new API key"},
		},
		{
			name: "both warnings reach the user",
			decide: keep(APIKeyDecision{Reason: "the stored API key expires soon", Fallback: true, ExpiresAt: expiresAt,
				Warning: "could not verify the stored API key: no route"}),
			wantKeep:      true,
			wantExpiresAt: expiresAt,
			wantWarnings:  []string{"could not verify the stored API key", "could not create a new API key"},
		},
		{
			name:         "a stored key with no recorded expiry that still works is kept",
			decide:       keep(APIKeyDecision{Reason: "the expiry of the stored API key is not recorded", Fallback: true}),
			wantKeep:     true,
			wantWarnings: []string{"could not create a new API key"},
		},
		{
			// The OAuth tokens of the login still work, so the caller saves
			// them and reports the error.
			name: "a fallback that expired during the create leaves no key",
			decide: keep(APIKeyDecision{Reason: "the stored API key expires soon", Fallback: true,
				ExpiresAt: time.Now().Add(-time.Second)}),
			wantCreateError: true,
		},
		{
			name:            "no usable stored key leaves no key",
			decide:          keep(APIKeyDecision{Reason: "the stored API key expired"}),
			wantCreateError: true,
		},
		{
			name:    "--rotate-api-key fails the login",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			connFor := newBootstrapTestServer(t, bootstrapTestServer{
				user:       &fakeUserServer{responses: [][]string{{"acme.safedep.io"}}},
				onboarding: &fakeOnboardingServer{},
				apiKeys:    &fakeAPIKeyServer{err: status.Error(codes.PermissionDenied, "member role required")},
			})

			res, err := PostOAuthBootstrap(context.Background(), BootstrapInput{
				AccessToken:      "token-123",
				CreateAPIKey:     true,
				APIKeyExpiryDays: 90,
				DecideAPIKey:     tc.decide,
				ConnFor:          connFor,
			})

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, res.APIKey)
			assert.Equal(t, tc.wantKeep, res.APIKeyDecision.Keep)
			if tc.wantKeep {
				assert.True(t, tc.wantExpiresAt.Equal(res.APIKeyDecision.ExpiresAt))
			}
			for _, w := range tc.wantWarnings {
				assert.Contains(t, res.APIKeyDecision.Warning, w)
			}
			if tc.wantCreateError {
				assert.Equal(t, codes.PermissionDenied, status.Code(res.APIKeyError))
			} else {
				assert.NoError(t, res.APIKeyError)
			}
		})
	}
}
