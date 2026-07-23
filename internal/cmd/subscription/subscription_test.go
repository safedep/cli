package subscription

import (
	"context"
	"testing"
	"time"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeSvc satisfies every subscription interface; tests wire only the funcs
// they exercise.
type fakeSvc struct {
	statusFn     func(context.Context) (*AccountStatus, error)
	getCustFn    func(context.Context) (*Customer, bool, error)
	createCustFn func(context.Context, CustomerInput) (*Customer, []ProviderError, error)
	activateFn   func(context.Context) error
	checkoutFn   func(context.Context, CheckoutInput) (*CheckoutResult, error)
}

func (f *fakeSvc) Status(ctx context.Context) (*AccountStatus, error) { return f.statusFn(ctx) }
func (f *fakeSvc) GetCustomer(ctx context.Context) (*Customer, bool, error) {
	return f.getCustFn(ctx)
}
func (f *fakeSvc) CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, []ProviderError, error) {
	return f.createCustFn(ctx, in)
}
func (f *fakeSvc) ActivateTrial(ctx context.Context) error { return f.activateFn(ctx) }
func (f *fakeSvc) Checkout(ctx context.Context, in CheckoutInput) (*CheckoutResult, error) {
	return f.checkoutFn(ctx, in)
}

func customerExists(*fakeSvc) func(context.Context) (*Customer, bool, error) {
	return func(context.Context) (*Customer, bool, error) { return &Customer{Name: "Acme"}, true, nil }
}

func TestRegister_buildsSubscriptionTree(t *testing.T) {
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)
	root := &cobra.Command{Use: "safedep"}
	Register(root, a)

	for _, path := range [][]string{
		{"subscription"}, {"subscription", "status"},
		{"subscription", "trial", "enable"}, {"subscription", "create"},
		{"subscription", "ondemand", "enable"}, {"subscription", "ondemand", "disable"}, {"subscription", "ondemand", "status"},
		{"subscription", "customer", "create"}, {"subscription", "customer", "show"},
		{"subscription", "portal"},
	} {
		cmd, _, err := root.Find(path)
		require.NoError(t, err, path)
		require.NotNil(t, cmd, path)
		assert.NotEmpty(t, cmd.Short, path)
		assert.NotEmpty(t, cmd.Long, path)
	}

	create, _, _ := root.Find([]string{"subscription", "create"})
	assert.NotNil(t, create.Flags().Lookup("seats"))
	enable, _, _ := root.Find([]string{"subscription", "ondemand", "enable"})
	assert.NotNil(t, enable.Flags().Lookup("accept-terms"))
}

func TestEnumTokens(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "active-trial", statusToken(1)) // SUBSCRIPTION_ACCOUNT_STATUS_ACTIVE_TRIAL
	assert.Equal(t, "professional", tierToken(2))   // BILLING_TIER_PROFESSIONAL
	assert.Equal(t, "unknown", tierToken(0))        // UNSPECIFIED
	assert.Equal(t, "tool-sync", featureToken(3))   // FEATURE_TOOL_SYNC
	assert.Equal(t, "ok", postureToken(1))          // PAYMENT_POSTURE_OK
}

func TestMapOnDemandEnableError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason errorv1.ErrorReason
		want   string
	}{
		{"entitlement", errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE, "paid plan"},
		{"payment", errorv1.ErrorReason_ERROR_REASON_PAYMENT_METHOD_REQUIRED, "payment method"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detail := &errorv1.ErrorDetail{}
			detail.SetReason(tt.reason)
			st, err := status.New(codes.FailedPrecondition, "denied").WithDetails(detail)
			require.NoError(t, err)
			assert.Contains(t, mapOnDemandEnableError(st.Err()).Error(), tt.want)
		})
	}
}

func TestMapOnDemandEnableError_UntypedPassthrough(t *testing.T) {
	t.Parallel()
	err := status.Error(codes.Unavailable, "boom")
	assert.Contains(t, mapOnDemandEnableError(err).Error(), "enable on-demand")
}

func TestEnsureCustomer_ExistsIsNoop(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{getCustFn: customerExists(nil)}
	require.NoError(t, ensureCustomer(context.Background(), svc, customerForm{}))
}

func TestEnsureCustomer_MissingFlagsErrors(t *testing.T) {
	t.Parallel()
	// Non-TTY test env -> not interactive -> missing required flags error.
	svc := &fakeSvc{getCustFn: func(context.Context) (*Customer, bool, error) { return nil, false, nil }}
	err := ensureCustomer(context.Background(), svc, customerForm{Name: "Acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing billing details")
}

func TestEnsureCustomer_CreatesFromFlags(t *testing.T) {
	t.Parallel()
	var got CustomerInput
	svc := &fakeSvc{
		getCustFn: func(context.Context) (*Customer, bool, error) { return nil, false, nil },
		createCustFn: func(_ context.Context, in CustomerInput) (*Customer, []ProviderError, error) {
			got = in
			return &Customer{Name: in.Name}, nil, nil
		},
	}
	form := customerForm{Name: "Acme", Phone: "+1", Country: "US", State: "CA", City: "SF", Postal: "94105", Line1: "500 Howard"}
	require.NoError(t, ensureCustomer(context.Background(), svc, form))
	assert.Equal(t, "Acme", got.Name)
	assert.Equal(t, "US", got.Country)
}

func TestRunTrialEnable_NoWait(t *testing.T) {
	t.Parallel()
	activated := false
	svc := &fakeSvc{
		getCustFn:  customerExists(nil),
		activateFn: func(context.Context) error { activated = true; return nil },
		statusFn:   func(context.Context) (*AccountStatus, error) { return &AccountStatus{Status: statusActiveTrial}, nil },
	}
	acct, confirmed, err := runTrialEnable(context.Background(), svc, customerForm{}, false, time.Minute)
	require.NoError(t, err)
	assert.True(t, activated)
	assert.True(t, confirmed, "observed trial status confirms activation")
	assert.Equal(t, statusActiveTrial, acct.Status)
}

func TestRunTrialEnable_NotConfirmedWhenStillFree(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		getCustFn:  customerExists(nil),
		activateFn: func(context.Context) error { return nil },
		statusFn:   func(context.Context) (*AccountStatus, error) { return &AccountStatus{Status: statusFree}, nil },
	}
	acct, confirmed, err := runTrialEnable(context.Background(), svc, customerForm{}, false, time.Minute)
	require.NoError(t, err)
	assert.False(t, confirmed, "still-free account is not a confirmed activation")
	assert.Equal(t, statusFree, acct.Status)
}

func TestRunCreate_AlreadyActive(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		getCustFn: customerExists(nil),
		checkoutFn: func(context.Context, CheckoutInput) (*CheckoutResult, error) {
			return &CheckoutResult{Outcome: checkoutSuccess}, nil
		},
		statusFn: func(context.Context) (*AccountStatus, error) { return &AccountStatus{Status: statusActive}, nil },
	}
	res, err := runCreate(context.Background(), svc, customerForm{}, 5, true, time.Minute)
	require.NoError(t, err)
	assert.True(t, res.alreadyActive)
	assert.Equal(t, statusActive, res.acct.Status)
}

func TestRunCreate_CheckoutError(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		getCustFn: customerExists(nil),
		checkoutFn: func(context.Context, CheckoutInput) (*CheckoutResult, error) {
			return &CheckoutResult{Outcome: checkoutError, ErrorCode: "card_declined", ErrorMessage: "declined"}, nil
		},
	}
	_, err := runCreate(context.Background(), svc, customerForm{}, 5, true, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declined")
}

func TestRunCreate_NeedsCheckoutNoWait(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		getCustFn: customerExists(nil),
		checkoutFn: func(_ context.Context, in CheckoutInput) (*CheckoutResult, error) {
			assert.Equal(t, uint32(10), in.Seats)
			return &CheckoutResult{Outcome: checkoutNeeded, URL: "https://checkout.example/x"}, nil
		},
	}
	res, err := runCreate(context.Background(), svc, customerForm{}, 10, false, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "https://checkout.example/x", res.checkoutURL)
	assert.Nil(t, res.acct)
}

func TestCreateCmd_SeatsMinValidation(t *testing.T) {
	t.Parallel()
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)
	cmd := createCmd(a)
	cmd.SetArgs([]string{"--seats", "0"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 1")
}

func TestStatusResult_Render(t *testing.T) {
	t.Parallel()
	acct := &AccountStatus{
		Status: statusActiveTrial, Tier: "professional",
		Trial:        &TrialInfo{DaysRemaining: 14, ExpiresAt: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)},
		Entitlements: []string{"tool-sync", "sql-query"},
		OnDemand:     &OnDemandState{Enabled: false},
	}
	// Default: entitlements are hidden.
	def := &statusResult{acct: acct}
	tbl := def.RenderTable()
	assert.Contains(t, tbl, "ACTIVE-TRIAL")
	assert.Contains(t, tbl, "Subscribe anytime")
	assert.NotContains(t, tbl, "tool-sync", "entitlements are opt-in")
	js, err := def.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js), "\"days_remaining\": 14")
	assert.NotContains(t, string(js), "entitlements")

	// Opt-in: --entitlements surfaces them in table and json.
	on := &statusResult{acct: acct, showEntitlements: true}
	assert.Contains(t, on.RenderTable(), "tool-sync")
	js2, err := on.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js2), "tool-sync")
}

func TestOndemandResult_Render(t *testing.T) {
	t.Parallel()
	r := &ondemandResult{state: &OnDemandState{Enabled: true, PaymentMethodOnFile: true, Posture: "ok"}}
	assert.Contains(t, r.RenderTable(), "enabled")
	js, err := r.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js), "\"payment_posture\": \"ok\"")
}
