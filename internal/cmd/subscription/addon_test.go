package subscription

import (
	"context"
	"testing"
	"time"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const threatIntelAddOn = "threat-intel-feed"

func TestParseAddOn(t *testing.T) {
	t.Parallel()

	code, err := parseAddOn(threatIntelAddOn)
	require.NoError(t, err)
	assert.Equal(t, threatIntelAddOn, addOnToken(code), "token round-trips through the enum")

	_, err = parseAddOn("bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown add-on")
	assert.Contains(t, err.Error(), threatIntelAddOn, "the error lists the valid tokens")
}

func TestAddOnTokens_ExcludesUnspecified(t *testing.T) {
	t.Parallel()
	tokens := AddOnTokens()
	assert.Contains(t, tokens, threatIntelAddOn)
	assert.NotContains(t, tokens, "unknown")
}

func TestMapAddOnAttachError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason errorv1.ErrorReason
		want   string
	}{
		{"entitlement", errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE, "paid plan"},
		{"past due", errorv1.ErrorReason_ERROR_REASON_SUBSCRIPTION_PAST_DUE, "past due"},
		{"payment", errorv1.ErrorReason_ERROR_REASON_PAYMENT_METHOD_REQUIRED, "payment method"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detail := &errorv1.ErrorDetail{}
			detail.SetReason(tt.reason)
			st, err := status.New(codes.FailedPrecondition, "denied").WithDetails(detail)
			require.NoError(t, err)
			assert.Contains(t, mapAddOnAttachError(st.Err()).Error(), tt.want)
		})
	}
}

func TestMapAddOnAttachError_UntypedPassthrough(t *testing.T) {
	t.Parallel()
	err := status.Error(codes.Unavailable, "boom")
	assert.Contains(t, mapAddOnAttachError(err).Error(), "add add-on")
}

func TestRunAddonAdd_WaitConfirmsPresence(t *testing.T) {
	t.Parallel()
	var gotTerms string
	svc := &fakeSvc{
		attachFn: func(_ context.Context, _, terms string) ([]string, error) {
			gotTerms = terms
			// The RPC unions optimistically, but the account items still lag.
			return []string{threatIntelAddOn}, nil
		},
		statusFn: func(context.Context) (*AccountStatus, error) {
			return &AccountStatus{Status: statusActive, ActiveAddOns: []string{threatIntelAddOn}}, nil
		},
	}
	res, err := runAddonAdd(context.Background(), svc, threatIntelAddOn, true, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, []string{threatIntelAddOn}, res.addOns)
	assert.Equal(t, termsVersion, gotTerms, "the shipped terms version is recorded")
}

func TestRunAddonAdd_NoWaitSkipsStatus(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		attachFn: func(context.Context, string, string) ([]string, error) {
			return []string{threatIntelAddOn}, nil
		},
		statusFn: func(context.Context) (*AccountStatus, error) {
			t.Fatal("no-wait must not read status")
			return nil, nil
		},
	}
	res, err := runAddonAdd(context.Background(), svc, threatIntelAddOn, false, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, []string{threatIntelAddOn}, res.addOns)
}

func TestRunAddonAdd_WaitTimeoutErrors(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		attachFn: func(context.Context, string, string) ([]string, error) { return []string{threatIntelAddOn}, nil },
		// Status never reflects the add-on: the webhook never lands.
		statusFn: func(context.Context) (*AccountStatus, error) { return &AccountStatus{Status: statusActive}, nil },
	}
	_, err := runAddonAdd(context.Background(), svc, threatIntelAddOn, true, time.Nanosecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestRunAddonRemove_WaitConfirmsAbsence(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		detachFn: func(context.Context, string) ([]string, error) { return nil, nil },
		statusFn: func(context.Context) (*AccountStatus, error) {
			return &AccountStatus{Status: statusActive}, nil // no add-ons
		},
	}
	res, err := runAddonRemove(context.Background(), svc, threatIntelAddOn, true, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, res.addOns)
}

func TestRunCreate_PassesAddOnsToCheckout(t *testing.T) {
	t.Parallel()
	var got []string
	svc := &fakeSvc{
		getCustFn: customerExists(nil),
		checkoutFn: func(_ context.Context, in CheckoutInput) (*CheckoutResult, error) {
			got = in.AddOns
			return &CheckoutResult{Outcome: checkoutNeeded, URL: "u"}, nil
		},
	}
	_, err := runCreate(context.Background(), svc, customerForm{}, 5, []string{threatIntelAddOn}, false, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, []string{threatIntelAddOn}, got)
}

func TestRunCreate_RejectsBadAddOnBeforeCustomer(t *testing.T) {
	t.Parallel()
	svc := &fakeSvc{
		getCustFn: func(context.Context) (*Customer, bool, error) {
			t.Fatal("a bad add-on token must be rejected before any customer call")
			return nil, false, nil
		},
	}
	_, err := runCreate(context.Background(), svc, customerForm{}, 5, []string{"bogus"}, false, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown add-on")
}

func TestAddonAddCmd_RequiresAcceptTerms(t *testing.T) {
	t.Parallel()
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)
	cmd := addonAddCmd(a)
	cmd.SetArgs([]string{threatIntelAddOn})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accept-terms")
}

func TestAddonAddCmd_RejectsUnknownAddOn(t *testing.T) {
	t.Parallel()
	a := app.New(&config.Config{})
	t.Cleanup(a.Close)
	cmd := addonAddCmd(a)
	cmd.SetArgs([]string{"bogus"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown add-on")
}

func TestAddonResults_Render(t *testing.T) {
	t.Parallel()

	empty := &addonListResult{}
	assert.Contains(t, empty.RenderTable(), "Buy one")
	js, err := empty.RenderJSON()
	require.NoError(t, err)
	assert.Contains(t, string(js), "\"add_ons\": []")

	full := &addonMutationResult{addOns: []string{threatIntelAddOn}}
	assert.Contains(t, full.RenderTable(), threatIntelAddOn)
	assert.Contains(t, full.RenderPlain(), "add_on\t"+threatIntelAddOn)
}
