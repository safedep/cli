package subscription

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ctv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	msgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	ctv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/cli/internal/tui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// termsVersion is the billing terms version the CLI records as accepted, for
// both on-demand billing and add-on purchases. Both accept the same terms
// document (termsURL) and there is no terms content/version API yet, so this is
// a single shipped constant. See the spec's Future section.
const termsVersion = "2026-07-23"

// One narrow interface per operation so commands and tests depend only on
// what they use. Service is the single gRPC-backed implementation over both
// the subscription and billing service clients.

type StatusGetter interface {
	Status(ctx context.Context) (*AccountStatus, error)
}

type TrialActivator interface {
	ActivateTrial(ctx context.Context) error
}

type CustomerGetter interface {
	// GetCustomer returns the billing customer and whether one exists. A
	// missing customer is (nil, false, nil), not an error.
	GetCustomer(ctx context.Context) (*Customer, bool, error)
}

type CustomerCreator interface {
	CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, []ProviderError, error)
}

type Subscriber interface {
	Checkout(ctx context.Context, in CheckoutInput) (*CheckoutResult, error)
}

type PortalOpener interface {
	Portal(ctx context.Context, returnURL string) (string, error)
}

type OnDemandStateGetter interface {
	OnDemandState(ctx context.Context) (*OnDemandState, error)
}

type OnDemandEnabler interface {
	EnableOnDemand(ctx context.Context, terms string) (*OnDemandState, error)
}

type OnDemandDisabler interface {
	DisableOnDemand(ctx context.Context) (*OnDemandState, error)
}

type AddOnAttacher interface {
	// AttachAddOn purchases the add-on on the tenant's subscription and returns
	// the account's active add-on tokens after the change.
	AttachAddOn(ctx context.Context, addOn string, terms string) ([]string, error)
}

type AddOnDetacher interface {
	// DetachAddOn removes the add-on and returns the remaining active add-on
	// tokens.
	DetachAddOn(ctx context.Context, addOn string) ([]string, error)
}

type CatalogGetter interface {
	// Catalog returns the price catalog: list prices for sellable products.
	Catalog(ctx context.Context) (*Catalog, error)
}

type Service struct {
	sub     ctv1grpc.SubscriptionServiceClient
	billing ctv1grpc.BillingServiceClient
}

func NewService(conn *grpc.ClientConn) *Service {
	return &Service{
		sub:     ctv1grpc.NewSubscriptionServiceClient(conn),
		billing: ctv1grpc.NewBillingServiceClient(conn),
	}
}

var (
	_ StatusGetter        = (*Service)(nil)
	_ TrialActivator      = (*Service)(nil)
	_ CustomerGetter      = (*Service)(nil)
	_ CustomerCreator     = (*Service)(nil)
	_ Subscriber          = (*Service)(nil)
	_ PortalOpener        = (*Service)(nil)
	_ OnDemandStateGetter = (*Service)(nil)
	_ OnDemandEnabler     = (*Service)(nil)
	_ OnDemandDisabler    = (*Service)(nil)
	_ AddOnAttacher       = (*Service)(nil)
	_ AddOnDetacher       = (*Service)(nil)
	_ CatalogGetter       = (*Service)(nil)
)

// CLI-side types. Proto stays out of command code.

type TrialInfo struct {
	DaysRemaining int32
	ExpiresAt     time.Time
}

// Catalog is the price catalog: list prices for every sellable product. The
// prices are the same for every tenant and carry no tenant-specific discount.
type Catalog struct {
	Products []CatalogProduct
}

type CatalogProduct struct {
	DisplayName string
	Kind        string // "subscription_tier" | "add_on" | "overage"
	AddOn       string // add-on token when Kind is "add_on", else ""
	PricingUnit string // selling unit ("seat", "scan"), empty for a flat product
	Prices      []CatalogPrice
}

type CatalogPrice struct {
	UnitAmountMinor int64
	Currency        string
	Interval        string // "monthly" | "yearly" | ""
	Metered         bool
}

type AccountStatus struct {
	Status       string
	Tier         string
	Seats        int64      // effective seat count. Zero means no seat-based subscription.
	Interval     string     // tier billing cadence: "monthly" | "yearly" | ""
	ActiveAddOns []string   // purchased add-on tokens; entitlements cover manual grants separately
	Trial        *TrialInfo // set only when in an active trial
	Entitlements []string
	OnDemand     *OnDemandState // best-effort; nil if unavailable
	Endpoints    *EndpointUsage // best-effort; nil when the server omits it
}

// EndpointUsage is the account's SDLC Endpoint consumption for the current
// billing period. Numbers come from the server as-is: the CLI performs no
// billing math.
type EndpointUsage struct {
	UnitsUsed     int64
	UnitsIncluded int64 // meaningful only when HasIncluded
	HasIncluded   bool  // false when the account has no defined allotment
	Breakdown     []AssetClassUsage
	PeriodEnd     time.Time // reset instant; zero when the server omits it
}

// AssetClassUsage is one asset class row of the endpoint usage breakdown. It
// is mandatory rendering data: the rows explain the units total.
type AssetClassUsage struct {
	DisplayName   string
	ActiveAssets  int64
	AssetsPerUnit int64
	Units         int64
}

type OnDemandState struct {
	Enabled             bool
	PaymentMethodOnFile bool
	Posture             string
	Usage               []FeatureUsage
}

// FeatureUsage is the CLI-side view of one metered feature's usage. Numbers are
// copied from the API as-is; the CLI performs no billing math.
type FeatureUsage struct {
	FeatureKey        string
	DisplayName       string
	UnitLabel         string
	IncludedLimit     int64 // -1 unlimited, 0 not-available, >0 finite
	Consumed          int64
	Seats             int64
	Tier              string
	PeriodEnd         time.Time
	Provisional       bool // enforcement == PROVISIONAL
	Overage           *FeatureOverage
	OverageUsed       int64
	OverageUsedMinor  int64
	SettlementPending bool // settlement_status == PENDING
}

type FeatureOverage struct {
	CapKind        string // "units" | "monetary"
	CapUnits       int64
	UnitPriceMinor int64
	CapAmountMinor int64
	Currency       string
}

type CheckoutInput struct {
	Seats      uint32
	AddOns     []string // add-on tokens to buy alongside the seats
	SuccessURL string
	CancelURL  string
}

// Checkout outcome tokens.
const (
	checkoutSuccess = "success"
	checkoutNeeded  = "need-checkout"
	checkoutError   = "error"
)

type CheckoutResult struct {
	Outcome      string // checkoutSuccess | checkoutNeeded | checkoutError
	URL          string
	ErrorCode    string
	ErrorMessage string
}

type Customer struct {
	ID      string
	Name    string
	Email   string
	Phone   string
	Country string
	State   string
	City    string
	Postal  string
	Line1   string
	Line2   string
	TaxID   string
}

type CustomerInput struct {
	Name    string
	Phone   string
	Country string
	State   string
	City    string
	Postal  string
	Line1   string
	Line2   string
	TaxID   string
}

type ProviderError struct {
	Type    string
	Param   string
	Message string
}

func (s *Service) Status(ctx context.Context) (*AccountStatus, error) {
	res, err := s.sub.GetSubscriptionAccountStatus(ctx, &ctv1.GetSubscriptionAccountStatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("subscription: status: %w", err)
	}
	out := &AccountStatus{Status: statusToken(res.GetStatus())}
	if info := res.GetSubscriptionAccountInfo(); info != nil {
		out.Tier = tierToken(info.GetBillingTier())
		out.Seats = int64(info.GetSeats())
		out.Interval = intervalToken(info.GetBillingInterval())
		for _, a := range info.GetActiveAddOns() {
			out.ActiveAddOns = append(out.ActiveAddOns, addOnToken(a))
		}
	}
	if t := res.GetTrialStatus(); t != nil {
		out.Trial = &TrialInfo{DaysRemaining: t.GetDaysRemaining()}
		if t.GetExpiresAt() != nil {
			out.Trial.ExpiresAt = t.GetExpiresAt().AsTime()
		}
	}
	for _, e := range res.GetEntitlements() {
		out.Entitlements = append(out.Entitlements, featureToken(e.GetEntitlement().GetFeature()))
	}
	if u := res.GetEndpointUsage(); u != nil {
		out.Endpoints = endpointUsageFromProto(u)
	}
	// On-demand summary is best-effort: a failure here must not fail status.
	if st, err := s.OnDemandState(ctx); err == nil {
		out.OnDemand = st
	}
	return out, nil
}

func endpointUsageFromProto(u *msgv1.SdlcEndpointUsage) *EndpointUsage {
	out := &EndpointUsage{
		UnitsUsed:   u.GetUnitsUsed(),
		HasIncluded: u.HasUnitsIncluded(),
	}
	if out.HasIncluded {
		out.UnitsIncluded = u.GetUnitsIncluded()
	}
	if u.GetPeriodEnd() != nil {
		out.PeriodEnd = u.GetPeriodEnd().AsTime()
	}
	for _, row := range u.GetBreakdown() {
		out.Breakdown = append(out.Breakdown, AssetClassUsage{
			DisplayName:   row.GetDisplayName(),
			ActiveAssets:  row.GetActiveAssets(),
			AssetsPerUnit: row.GetAssetsPerUnit(),
			Units:         row.GetUnits(),
		})
	}
	return out
}

func (s *Service) ActivateTrial(ctx context.Context) error {
	_, err := s.sub.ActivateTrialSubscription(ctx, &ctv1.ActivateTrialSubscriptionRequest{})
	if err != nil {
		return fmt.Errorf("subscription: activate trial: %w", err)
	}
	return nil
}

func (s *Service) GetCustomer(ctx context.Context) (*Customer, bool, error) {
	res, err := s.billing.GetBillingCustomer(ctx, &ctv1.GetBillingCustomerRequest{})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("subscription: get customer: %w", err)
	}
	return customerFromProto(res.GetBillingCustomer()), true, nil
}

func (s *Service) CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, []ProviderError, error) {
	req := &ctv1.CreateBillingCustomerRequest{}
	req.SetCustomerName(in.Name)
	req.SetCustomerPhone(in.Phone)
	req.SetCustomerBillingAddressCountry(in.Country)
	req.SetCustomerBillingAddressState(in.State)
	req.SetCustomerBillingAddressCity(in.City)
	req.SetCustomerBillingAddressPostalCode(in.Postal)
	req.SetCustomerBillingAddressLine_1(in.Line1)
	if in.Line2 != "" {
		req.SetCustomerBillingAddressLine_2(in.Line2)
	}
	if in.TaxID != "" {
		req.SetCustomerTaxId(in.TaxID)
	}
	res, err := s.billing.CreateBillingCustomer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("subscription: create customer: %w", err)
	}
	var perr []ProviderError
	for _, e := range res.GetErrors() {
		perr = append(perr, ProviderError{Type: e.GetType(), Param: e.GetParam(), Message: e.GetMessage()})
	}
	return customerFromProto(res.GetBillingCustomer()), perr, nil
}

func (s *Service) Checkout(ctx context.Context, in CheckoutInput) (*CheckoutResult, error) {
	flow := &ctv1.CreateBillingSubscriptionCheckoutSessionRequest_FlowInfo{}
	flow.SetSuccessUrl(in.SuccessURL)
	flow.SetCancelUrl(in.CancelURL)

	req := &ctv1.CreateBillingSubscriptionCheckoutSessionRequest{}
	req.SetBillingTier(msgv1.BillingTier_BILLING_TIER_PROFESSIONAL)
	req.SetFlowInfo(flow)
	if in.Seats > 0 {
		req.SetQuantity(in.Seats)
	}
	if len(in.AddOns) > 0 {
		addOns, err := parseAddOns(in.AddOns)
		if err != nil {
			return nil, err
		}
		req.SetAddOns(addOns)
	}
	res, err := s.billing.CreateBillingSubscriptionCheckoutSession(ctx, req)
	if err != nil {
		return nil, mapCheckoutError(err)
	}
	info := res.GetStatusInfo()
	out := &CheckoutResult{URL: res.GetCheckoutSessionUrl()}
	switch info.GetStatus() {
	case ctv1.CreateBillingSubscriptionCheckoutSessionResponse_STATUS_SUCCESS:
		out.Outcome = checkoutSuccess
	case ctv1.CreateBillingSubscriptionCheckoutSessionResponse_STATUS_NEED_CHECKOUT_COMPLETION:
		out.Outcome = checkoutNeeded
	default:
		out.Outcome = checkoutError
		out.ErrorCode = info.GetErrorCode()
		out.ErrorMessage = info.GetErrorMessage()
	}
	return out, nil
}

func (s *Service) Portal(ctx context.Context, returnURL string) (string, error) {
	flow := &ctv1.CreateBillingCustomerPortalSessionRequest_FlowInfo{}
	flow.SetReturnUrl(returnURL)
	req := &ctv1.CreateBillingCustomerPortalSessionRequest{}
	req.SetFlowInfo(flow)
	res, err := s.billing.CreateBillingCustomerPortalSession(ctx, req)
	if err != nil {
		return "", fmt.Errorf("subscription: portal: %w", err)
	}
	return res.GetCustomerPortalUrl(), nil
}

func (s *Service) OnDemandState(ctx context.Context) (*OnDemandState, error) {
	res, err := s.billing.GetOnDemandBillingState(ctx, &ctv1.GetOnDemandBillingStateRequest{})
	if err != nil {
		return nil, fmt.Errorf("subscription: on-demand state: %w", err)
	}
	st := onDemandFromProto(res.GetState())
	st.Usage = featureUsageFromProto(res.GetFeatureUsage())
	return st, nil
}

func (s *Service) EnableOnDemand(ctx context.Context, terms string) (*OnDemandState, error) {
	req := &ctv1.EnableOnDemandBillingRequest{}
	req.SetTermsVersion(terms)
	res, err := s.billing.EnableOnDemandBilling(ctx, req)
	if err != nil {
		return nil, mapOnDemandEnableError(err)
	}
	return onDemandFromProto(res.GetState()), nil
}

func (s *Service) DisableOnDemand(ctx context.Context) (*OnDemandState, error) {
	res, err := s.billing.DisableOnDemandBilling(ctx, &ctv1.DisableOnDemandBillingRequest{})
	if err != nil {
		return nil, fmt.Errorf("subscription: disable on-demand: %w", err)
	}
	return onDemandFromProto(res.GetState()), nil
}

func (s *Service) AttachAddOn(ctx context.Context, addOn string, terms string) ([]string, error) {
	code, err := parseAddOn(addOn)
	if err != nil {
		return nil, err
	}
	req := &ctv1.AddBillingSubscriptionAddOnRequest{}
	req.SetAddOn(code)
	req.SetTermsVersion(terms)
	res, err := s.billing.AddBillingSubscriptionAddOn(ctx, req)
	if err != nil {
		return nil, mapAddOnAttachError(err)
	}
	return addOnTokens(res.GetActiveAddOns()), nil
}

func (s *Service) DetachAddOn(ctx context.Context, addOn string) ([]string, error) {
	code, err := parseAddOn(addOn)
	if err != nil {
		return nil, err
	}
	req := &ctv1.RemoveBillingSubscriptionAddOnRequest{}
	req.SetAddOn(code)
	res, err := s.billing.RemoveBillingSubscriptionAddOn(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("subscription: remove add-on: %w", err)
	}
	return addOnTokens(res.GetActiveAddOns()), nil
}

func (s *Service) Catalog(ctx context.Context) (*Catalog, error) {
	res, err := s.billing.GetBillingCatalog(ctx, &ctv1.GetBillingCatalogRequest{})
	if err != nil {
		return nil, fmt.Errorf("subscription: get pricing: %w", err)
	}
	return catalogFromProto(res.GetCatalog()), nil
}

func catalogFromProto(pb *msgv1.BillingCatalog) *Catalog {
	out := &Catalog{Products: make([]CatalogProduct, 0, len(pb.GetProducts()))}
	for _, p := range pb.GetProducts() {
		product := CatalogProduct{
			DisplayName: p.GetDisplayName(),
			Kind:        productKindToken(p.GetKind()),
			PricingUnit: p.GetPricingUnit(),
			Prices:      make([]CatalogPrice, 0, len(p.GetPrices())),
		}
		if p.HasAddOn() {
			product.AddOn = addOnToken(p.GetAddOn())
		}
		for _, pr := range p.GetPrices() {
			product.Prices = append(product.Prices, CatalogPrice{
				UnitAmountMinor: pr.GetUnitAmountMinor(),
				Currency:        pr.GetCurrency(),
				Interval:        intervalToken(pr.GetInterval()),
				Metered:         pr.GetUsageType() == msgv1.PriceUsageType_PRICE_USAGE_TYPE_METERED,
			})
		}
		out.Products = append(out.Products, product)
	}
	return out
}

// mapAddOnAttachError routes the typed ErrorReason from an add-on purchase to
// an actionable message naming the next command, matching the on-demand enable
// path.
func mapAddOnAttachError(err error) error {
	switch errorReason(err) {
	case errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE:
		return errors.New("add-ons need an active paid plan: subscribe first with `safedep subscription create`")
	case errorv1.ErrorReason_ERROR_REASON_SUBSCRIPTION_PAST_DUE:
		return errors.New("subscription is past due: settle payment via `safedep subscription portal open`, then retry")
	case errorv1.ErrorReason_ERROR_REASON_PAYMENT_METHOD_REQUIRED:
		return errors.New("no payment method on file: add one via `safedep subscription portal open`, then retry")
	case errorv1.ErrorReason_ERROR_REASON_PAYMENT_DECLINED:
		return errors.New("the card on file was declined: update it via `safedep subscription portal open`, then retry")
	default:
		return fmt.Errorf("subscription: add add-on: %w", err)
	}
}

// mapCheckoutError turns the checkout RPC's canonical failures into actionable
// messages. AlreadyExists means the tenant already holds an active
// subscription, so point at status and portal instead of the generic conflict
// text a bare code would render.
func mapCheckoutError(err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
		return errors.New("this account already has an active subscription: check `safedep subscription status` or manage billing with `safedep subscription portal open`")
	}
	return fmt.Errorf("subscription: checkout: %w", err)
}

// mapOnDemandEnableError routes the typed ErrorReason to an actionable
// message so the user knows the next command to run.
func mapOnDemandEnableError(err error) error {
	switch errorReason(err) {
	case errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE:
		return errors.New("on-demand billing needs a paid plan: subscribe first with `safedep subscription create`")
	case errorv1.ErrorReason_ERROR_REASON_PAYMENT_METHOD_REQUIRED:
		return errors.New("no payment method on file: add one via `safedep subscription portal open`, then retry")
	default:
		return fmt.Errorf("subscription: enable on-demand: %w", err)
	}
}

// errorReason extracts the typed business ErrorReason from a gRPC status
// error's details, or UNSPECIFIED when none is present.
func errorReason(err error) errorv1.ErrorReason {
	st, ok := status.FromError(err)
	if !ok {
		return errorv1.ErrorReason_ERROR_REASON_UNSPECIFIED
	}
	for _, d := range st.Details() {
		if detail, ok := d.(*errorv1.ErrorDetail); ok {
			return detail.GetReason()
		}
	}
	return errorv1.ErrorReason_ERROR_REASON_UNSPECIFIED
}

func customerFromProto(c *msgv1.BillingCustomer) *Customer {
	return &Customer{
		ID:      c.GetId(),
		Name:    c.GetCustomerName(),
		Email:   c.GetCustomerEmail(),
		Phone:   c.GetCustomerPhone(),
		Country: c.GetCustomerBillingAddressCountry(),
		State:   c.GetCustomerBillingAddressState(),
		City:    c.GetCustomerBillingAddressCity(),
		Postal:  c.GetCustomerBillingAddressPostalCode(),
		Line1:   c.GetCustomerBillingAddressLine_1(),
		Line2:   c.GetCustomerBillingAddressLine_2(),
		TaxID:   c.GetCustomerTaxId(),
	}
}

func onDemandFromProto(s *msgv1.TenantOnDemandBillingState) *OnDemandState {
	return &OnDemandState{
		Enabled:             s.GetOnDemandBillingEnabled(),
		PaymentMethodOnFile: s.GetPaymentMethodOnFile(),
		Posture:             postureToken(s.GetPaymentPosture()),
	}
}

func featureUsageFromProto(in []*msgv1.MeteredFeatureUsage) []FeatureUsage {
	if len(in) == 0 {
		return nil
	}
	out := make([]FeatureUsage, 0, len(in))
	for _, u := range in {
		fu := FeatureUsage{
			FeatureKey:        u.GetFeatureKey(),
			DisplayName:       u.GetDisplayName(),
			UnitLabel:         u.GetUnitLabel(),
			IncludedLimit:     u.GetIncludedLimit(),
			Consumed:          u.GetConsumed(),
			Seats:             u.GetSeats(),
			Tier:              tierToken(u.GetTier()),
			Provisional:       u.GetEnforcement() == msgv1.MeteredLimitEnforcement_METERED_LIMIT_ENFORCEMENT_PROVISIONAL,
			OverageUsed:       u.GetOverageUsed(),
			OverageUsedMinor:  u.GetOverageUsedAmountMinor(),
			SettlementPending: u.GetSettlementStatus() == msgv1.OverageSettlementStatus_OVERAGE_SETTLEMENT_STATUS_PENDING,
		}
		if u.GetPeriodEnd() != nil {
			fu.PeriodEnd = u.GetPeriodEnd().AsTime()
		}
		if ov := u.GetOverage(); ov != nil {
			fu.Overage = &FeatureOverage{
				CapKind:        capKindToken(ov.GetCapKind()),
				CapUnits:       ov.GetCapUnits(),
				UnitPriceMinor: ov.GetUnitPriceMinor(),
				CapAmountMinor: ov.GetCapAmountMinor(),
				Currency:       ov.GetCurrency(),
			}
		}
		out = append(out, fu)
	}
	return out
}

func capKindToken(k msgv1.OverageCapKind) string {
	return tui.EnumToken(k.String(), "OVERAGE_CAP_KIND_")
}

// Enum -> display token helpers, via the shared tui.EnumToken so new enum
// values render without code changes.

func statusToken(s msgv1.SubscriptionAccountStatus) string {
	return tui.EnumToken(s.String(), "SUBSCRIPTION_ACCOUNT_STATUS_")
}

// tierDisplayNames overrides the CLI-visible name for tiers whose product name
// diverges from the proto enum. The backend enum stays canonical (the CLI still
// sends BILLING_TIER_PROFESSIONAL); only the user-facing token changes. A tier
// absent from this map falls back to the generic enum token, so a new
// BILLING_TIER_* renders sensibly with no code change and gets a custom name
// here only when product naming diverges.
var tierDisplayNames = map[msgv1.BillingTier]string{
	msgv1.BillingTier_BILLING_TIER_PROFESSIONAL: "team",
}

func tierToken(t msgv1.BillingTier) string {
	if name, ok := tierDisplayNames[t]; ok {
		return name
	}
	return tui.EnumToken(t.String(), "BILLING_TIER_")
}

func featureToken(f msgv1.Feature) string {
	return tui.EnumToken(f.String(), "FEATURE_")
}

func postureToken(p msgv1.PaymentPosture) string {
	return tui.EnumToken(p.String(), "PAYMENT_POSTURE_")
}

// intervalToken maps the billing interval to its display token, or "" when
// unspecified (EnumToken renders the zero value as "unknown", but an absent
// interval must be empty so callers can omit it).
func intervalToken(i msgv1.BillingInterval) string {
	if i == msgv1.BillingInterval_BILLING_INTERVAL_UNSPECIFIED {
		return ""
	}
	return tui.EnumToken(i.String(), "BILLING_INTERVAL_")
}

func productKindToken(k msgv1.ProductKind) string {
	return tui.EnumToken(k.String(), "PRODUCT_KIND_")
}

// formatMoney renders minor currency units as a display amount. USD gets a "$"
// prefix; any other currency shows the amount with its ISO code, so an unknown
// currency is never mislabeled as dollars.
func formatMoney(minor int64, currency string) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	whole, cents := minor/100, minor%100
	if currency == "usd" {
		return fmt.Sprintf("%s$%d.%02d", sign, whole, cents)
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, whole, cents, strings.ToUpper(currency))
}

const addOnEnumPrefix = "BILLING_ADD_ON_"

func addOnToken(a msgv1.BillingAddOn) string {
	return tui.EnumToken(a.String(), addOnEnumPrefix)
}

func addOnTokens(in []msgv1.BillingAddOn) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, addOnToken(a))
	}
	return out
}

// AddOnTokens lists the selectable add-on tokens (every enum value except the
// unspecified zero), for command help and error messages.
func AddOnTokens() []string {
	return tui.EnumTokens(msgv1.BillingAddOn_name, addOnEnumPrefix)
}

// parseAddOn resolves a display token back to its BillingAddOn enum, listing
// the valid tokens on a miss.
func parseAddOn(token string) (msgv1.BillingAddOn, error) {
	n, ok := tui.ParseEnumToken(msgv1.BillingAddOn_name, addOnEnumPrefix, token)
	if !ok {
		return msgv1.BillingAddOn_BILLING_ADD_ON_UNSPECIFIED,
			fmt.Errorf("unknown add-on %q: valid add-ons are %s", token, strings.Join(AddOnTokens(), ", "))
	}
	return msgv1.BillingAddOn(n), nil
}

func parseAddOns(tokens []string) ([]msgv1.BillingAddOn, error) {
	out := make([]msgv1.BillingAddOn, 0, len(tokens))
	for _, t := range tokens {
		code, err := parseAddOn(t)
		if err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, nil
}
