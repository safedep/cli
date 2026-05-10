package endpoint

import (
	"context"
	"fmt"
	"strings"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Five small interfaces, one per RPC, so unit tests can pass focused
// stubs without instantiating the full client.

type StatsFetcher interface {
	Stats(ctx context.Context, in StatsInput) (*StatsResult, error)
}

type EndpointLister interface {
	List(ctx context.Context, in ListInput) (*ListResult, error)
}

type EndpointGetter interface {
	Get(ctx context.Context, in GetInput) (*GetResult, error)
}

type GuardEventLister interface {
	ListGuardEvents(ctx context.Context, in GuardEventsInput) (*GuardEventsResult, error)
}

type InventoryEventLister interface {
	ListInventoryEvents(ctx context.Context, in InventoryEventsInput) (*InventoryEventsResult, error)
}

// Service is the production implementation backed by a control-plane
// gRPC connection. Command code accepts the small interfaces above.
type Service struct {
	client controltowerv1grpc.EndpointManagementServiceClient
}

func NewService(conn *grpc.ClientConn) *Service {
	return &Service{client: controltowerv1grpc.NewEndpointManagementServiceClient(conn)}
}

// Compile-time interface guards.
var (
	_ StatsFetcher         = (*Service)(nil)
	_ EndpointLister       = (*Service)(nil)
	_ EndpointGetter       = (*Service)(nil)
	_ GuardEventLister     = (*Service)(nil)
	_ InventoryEventLister = (*Service)(nil)
)

// Input and result types. Keep proto types out of command code.

// Output structs carry proto enums (Capabilities, TrustLevel, Kind,
// Scope) directly. Renderers translate them via display helpers, so
// command code does not need to import the proto package for
// formatting. Inputs accept the same proto enums for symmetry.

type TimeWindow struct {
	// Both zero means "let the server apply defaults" (omit time_range).
	Start, End time.Time
}

// WindowFromDuration returns a trailing window of length `since` ending
// at `now`. A non-positive duration returns a zero TimeWindow, which
// toTimeRangePtr translates to "omit time_range" so the server applies
// its default window.
func WindowFromDuration(now time.Time, since time.Duration) TimeWindow {
	if since <= 0 {
		return TimeWindow{}
	}
	return TimeWindow{Start: now.Add(-since), End: now}
}

type StatsInput struct{ Window TimeWindow }
type StatsResult struct {
	TotalEndpoints   uint64
	ActiveEndpoints  uint64
	SilentEndpoints  uint64 // derived: total - active
	TotalEvents      uint64
	PMGBlockedEvents uint64
}

type ListInput struct {
	Window        TimeWindow
	Capabilities  []controltowerv1.EndpointCapability
	MinPMGBlocked uint64 // 0 = no filter
	PageSize      uint32
	PageToken     string
}
type ListEndpoint struct {
	ID               string
	Identifier       string // EndpointIdentity.Identifier (human label; falls back to hostname server-side)
	Hostname         string // EndpointIdentity.Metadata.Hostname
	OS               string // displayOS(EndpointIdentity.Metadata.Os)
	Arch             string // displayArch(EndpointIdentity.Metadata.Arch)
	LastSync         time.Time
	Capabilities     []controltowerv1.EndpointCapability
	PMGBlockedEvents uint64
	PMGTotalEvents   uint64
	InventoryEvents  uint64
}
type ListResult struct {
	Endpoints []ListEndpoint
	NextPage  string
}

type GetInput struct {
	EndpointID string
	Window     TimeWindow
}
type ToolEventVolume struct {
	Tool  string
	Count uint64
}
type GetResult struct {
	ID             string
	Identifier     string
	Hostname       string
	OS             string
	Arch           string
	LastSync       time.Time
	PerToolVolumes []ToolEventVolume
	LastInvocation *InvocationContext // nil if absent
}

// InvocationContext mirrors the fields carried on the proto's
// EndpointInvocationContext: WorkingDirectory, Command, plus presence
// flags for CI / agent context.
type InvocationContext struct {
	Command    string
	WorkingDir string
	HasCI      bool
	HasAgent   bool
}

type GuardAction string // CLI vocab: "blocked", "confirmed", "trusted", "cooldown-blocked"

type GuardEventsInput struct {
	Window       TimeWindow
	EndpointIDs  []string
	Actions      []GuardAction
	InvocationID string
	PageSize     uint32
	PageToken    string
}
type GuardEvent struct {
	Timestamp      time.Time
	EndpointID     string
	Tool           string      // outer header ToolName
	ToolVersion    string      // outer header ToolVersion
	Action         GuardAction
	// Verdict is the user-facing reason for a block: "malicious",
	// "suspicious", "cooldown", or "blocked". Empty for non-block actions.
	Verdict        string
	PackageName    string
	PackageVersion string
	Ecosystem      string
	InvocationID   string
	Raw            *controltowerv1.ListEndpointPackageGuardEventsResponse_PackageGuardEvent
}
type GuardEventsResult struct {
	Events   []GuardEvent
	NextPage string
}

type InventoryEventsInput struct {
	Window       TimeWindow
	EndpointIDs  []string
	ItemKinds    []messagescontroltowerv1.InventoryItemKind
	Scope        *messagescontroltowerv1.InventoryScope
	InvocationID string
	PageSize     uint32
	PageToken    string
}
type InventoryEvent struct {
	Timestamp    time.Time
	EndpointID   string
	Tool         string                                // outer header ToolName
	Kind         messagescontroltowerv1.InventoryItemKind
	ItemIdentity string                               // ItemObserved.ItemIdentity (dedup key)
	Name         string                               // ItemObserved.Name
	App          string                               // ItemObserved.App
	Scope        messagescontroltowerv1.InventoryScope
	ConfigPath   string
	Metadata     map[string]string
	InvocationID string
	// Raw exposes the original proto event so JSON output preserves
	// every field. Intentional v1 trade-off for the activity feed.
	Raw          *controltowerv1.ListEndpointInventoryEventsResponse_InventoryEvent
}
type InventoryEventsResult struct {
	Events   []InventoryEvent
	NextPage string
}

func (s *Service) Stats(ctx context.Context, in StatsInput) (*StatsResult, error) {
	req := &controltowerv1.GetEndpointsStatsRequest{}
	if tr := toTimeRangePtr(in.Window); tr != nil {
		req.SetTimeRange(tr)
	}
	res, err := s.client.GetEndpointsStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("endpoint: stats: %w", err)
	}
	st := res.GetStats()
	out := &StatsResult{
		TotalEndpoints:   st.GetTotalEndpointsCount(),
		ActiveEndpoints:  st.GetActiveEndpointsCount(),
		TotalEvents:      st.GetTotalEventsCount(),
		PMGBlockedEvents: st.GetPackageGuardBlockedEventsCount(),
	}
	if out.TotalEndpoints >= out.ActiveEndpoints {
		out.SilentEndpoints = out.TotalEndpoints - out.ActiveEndpoints
	}
	return out, nil
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	req := &controltowerv1.ListEndpointsRequest{}
	if tr := toTimeRangePtr(in.Window); tr != nil {
		req.SetTimeRange(tr)
	}
	if in.PageSize > 0 {
		req.SetPagination(newPagination(in.PageSize, in.PageToken))
	}
	if len(in.Capabilities) > 0 || in.MinPMGBlocked > 0 {
		f := &controltowerv1.ListEndpointsRequest_Filter{}
		if len(in.Capabilities) > 0 {
			f.SetCapabilities(in.Capabilities)
		}
		if in.MinPMGBlocked > 0 {
			f.SetMinPackageGuardBlockedEventsCount(in.MinPMGBlocked)
		}
		req.SetFilter(f)
	}
	res, err := s.client.ListEndpoints(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("endpoint: list: %w", err)
	}
	out := &ListResult{NextPage: res.GetPagination().GetNextPageToken()}
	for _, ep := range res.GetEndpoints() {
		identity := ep.GetIdentity()
		meta := identity.GetMetadata()
		out.Endpoints = append(out.Endpoints, ListEndpoint{
			ID:               ep.GetEndpointId(),
			Identifier:       identity.GetIdentifier(),
			Hostname:         meta.GetHostname(),
			OS:               displayOS(meta.GetOs()),
			Arch:             displayArch(meta.GetArch()),
			LastSync:         ep.GetLastSyncAt().AsTime(),
			Capabilities:     ep.GetCapabilities(),
			PMGBlockedEvents: ep.GetPackageGuardBlockedEventsCount(),
			PMGTotalEvents:   ep.GetPackageGuardEventsCount(),
			InventoryEvents:  ep.GetVetInventoryEventsCount(),
		})
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, in GetInput) (*GetResult, error) {
	req := &controltowerv1.GetEndpointRequest{}
	req.SetEndpointId(in.EndpointID)
	if tr := toTimeRangePtr(in.Window); tr != nil {
		req.SetTimeRange(tr)
	}
	res, err := s.client.GetEndpoint(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("endpoint: get %s: %w", in.EndpointID, err)
	}
	ep := res.GetEndpoint()
	identity := ep.GetIdentity()
	meta := identity.GetMetadata()
	out := &GetResult{
		ID:         ep.GetEndpointId(),
		Identifier: identity.GetIdentifier(),
		Hostname:   meta.GetHostname(),
		OS:         displayOS(meta.GetOs()),
		Arch:       displayArch(meta.GetArch()),
		LastSync:   ep.GetLastSyncAt().AsTime(),
	}
	for _, v := range ep.GetToolEventVolumes() {
		out.PerToolVolumes = append(out.PerToolVolumes, ToolEventVolume{
			Tool: v.GetToolName(), Count: v.GetEventCount(),
		})
	}
	if inv := ep.GetLastInvocationContext(); inv != nil {
		out.LastInvocation = &InvocationContext{
			Command:    inv.GetCommand(),
			WorkingDir: inv.GetWorkingDirectory(),
			HasCI:      inv.HasCi(),
			HasAgent:   inv.HasAgent(),
		}
	}
	return out, nil
}

func (s *Service) ListGuardEvents(ctx context.Context, in GuardEventsInput) (*GuardEventsResult, error) {
	req := &controltowerv1.ListEndpointPackageGuardEventsRequest{}
	if tr := toTimeRangePtr(in.Window); tr != nil {
		req.SetTimeRange(tr)
	}
	if in.PageSize > 0 {
		req.SetPagination(newPagination(in.PageSize, in.PageToken))
	}
	if len(in.EndpointIDs) > 0 || len(in.Actions) > 0 || in.InvocationID != "" {
		f := &controltowerv1.ListEndpointPackageGuardEventsRequest_Filter{}
		if len(in.EndpointIDs) > 0 {
			f.SetEndpointIds(in.EndpointIDs)
		}
		if in.InvocationID != "" {
			f.SetInvocationId(in.InvocationID)
		}
		if len(in.Actions) > 0 {
			pmg := &controltowerv1.ListEndpointPackageGuardEventsRequest_Filter_PmgFilter{}
			actions, err := mapPmgActions(in.Actions)
			if err != nil {
				return nil, err
			}
			pmg.SetPackageActions(actions)
			f.SetPmg(pmg)
		}
		req.SetFilter(f)
	}
	res, err := s.client.ListEndpointPackageGuardEvents(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("endpoint: guard events: %w", err)
	}
	out := &GuardEventsResult{NextPage: res.GetPagination().GetNextPageToken()}
	for _, e := range res.GetEvents() {
		ge := GuardEvent{
			Timestamp:    e.GetTimestamp().AsTime(),
			EndpointID:   e.GetEndpointId(),
			Tool:         e.GetToolName(),
			ToolVersion:  e.GetToolVersion(),
			InvocationID: e.GetInvocationId(),
			Raw:          e,
		}
		if pmg := e.GetPmgEvent(); pmg != nil {
			applyPmgPayload(&ge, pmg)
		}
		out.Events = append(out.Events, ge)
	}
	return out, nil
}

func (s *Service) ListInventoryEvents(ctx context.Context, in InventoryEventsInput) (*InventoryEventsResult, error) {
	req := &controltowerv1.ListEndpointInventoryEventsRequest{}
	if tr := toTimeRangePtr(in.Window); tr != nil {
		req.SetTimeRange(tr)
	}
	if in.PageSize > 0 {
		req.SetPagination(newPagination(in.PageSize, in.PageToken))
	}
	if len(in.EndpointIDs) > 0 || len(in.ItemKinds) > 0 || in.Scope != nil || in.InvocationID != "" {
		f := &controltowerv1.ListEndpointInventoryEventsRequest_Filter{}
		if len(in.EndpointIDs) > 0 {
			f.SetEndpointIds(in.EndpointIDs)
		}
		if in.InvocationID != "" {
			f.SetInvocationId(in.InvocationID)
		}
		if len(in.ItemKinds) > 0 || in.Scope != nil {
			vf := &controltowerv1.ListEndpointInventoryEventsRequest_Filter_VetFilter{}
			if len(in.ItemKinds) > 0 {
				vf.SetItemKinds(in.ItemKinds)
			}
			if in.Scope != nil {
				vf.SetScope(*in.Scope)
			}
			f.SetVet(vf)
		}
		req.SetFilter(f)
	}
	res, err := s.client.ListEndpointInventoryEvents(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("endpoint: inventory events: %w", err)
	}
	out := &InventoryEventsResult{NextPage: res.GetPagination().GetNextPageToken()}
	for _, e := range res.GetEvents() {
		ie := InventoryEvent{
			Timestamp:    e.GetTimestamp().AsTime(),
			EndpointID:   e.GetEndpointId(),
			Tool:         e.GetToolName(),
			InvocationID: e.GetInvocationId(),
			Raw:          e,
		}
		if v := e.GetVetEvent(); v != nil {
			applyVetPayload(&ie, v)
		}
		out.Events = append(out.Events, ie)
	}
	return out, nil
}

func toTimeRangePtr(w TimeWindow) *controltowerv1.EndpointManagementTimeRange {
	if w.Start.IsZero() && w.End.IsZero() {
		return nil
	}
	tr := &controltowerv1.EndpointManagementTimeRange{}
	tr.SetStart(timestamppb.New(w.Start))
	tr.SetEnd(timestamppb.New(w.End))
	return tr
}

// newPagination builds a PaginationRequest.
func newPagination(size uint32, token string) *messagescontroltowerv1.PaginationRequest {
	p := &messagescontroltowerv1.PaginationRequest{}
	p.SetPageSize(size)
	if token != "" {
		p.SetPageToken(token)
	}
	return p
}

func mapPmgActions(actions []GuardAction) ([]messagescontroltowerv1.PmgPackageAction, error) {
	out := make([]messagescontroltowerv1.PmgPackageAction, 0, len(actions))
	for _, a := range actions {
		switch a {
		case "blocked":
			out = append(out, messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED)
		case "confirmed":
			out = append(out, messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_CONFIRMED)
		case "trusted":
			out = append(out, messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_TRUSTED)
		case "cooldown-blocked":
			out = append(out, messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED)
		default:
			return nil, fmt.Errorf("unknown action %q (use blocked|confirmed|trusted|cooldown-blocked)", a)
		}
	}
	return out, nil
}

func applyPmgPayload(ge *GuardEvent, pmg *messagescontroltowerv1.PmgEvent) {
	d := pmg.GetPackageDecision()
	if d == nil {
		return
	}
	ge.Action = pmgActionToCLI(d.GetAction())
	ge.Verdict = verdictFor(d.GetAction(), d.GetIsMalware(), d.GetIsVerified())
	pv := d.GetPackageVersion()
	if pv == nil {
		return
	}
	ge.PackageVersion = pv.GetVersion()
	if pkg := pv.GetPackage(); pkg != nil {
		ge.PackageName = pkg.GetName()
		ge.Ecosystem = displayEcosystem(pkg.GetEcosystem())
	}
}

func verdictFor(a messagescontroltowerv1.PmgPackageAction, isMalware, isVerified bool) string {
	switch a {
	case messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED:
		return "cooldown"
	case messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED:
		switch {
		case isMalware && isVerified:
			return "malicious"
		case isMalware:
			return "suspicious"
		default:
			return "blocked"
		}
	default:
		return ""
	}
}

func pmgActionToCLI(a messagescontroltowerv1.PmgPackageAction) GuardAction {
	switch a {
	case messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED:
		return "blocked"
	case messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_CONFIRMED:
		return "confirmed"
	case messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_TRUSTED:
		return "trusted"
	case messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED:
		return "cooldown-blocked"
	default:
		return GuardAction(a.String())
	}
}

func applyVetPayload(ie *InventoryEvent, v *messagescontroltowerv1.VetInventoryEvent) {
	item := v.GetItemObserved()
	if item == nil {
		return
	}
	ie.Kind = item.GetKind()
	ie.ItemIdentity = item.GetItemIdentity()
	ie.Name = item.GetName()
	ie.App = item.GetApp()
	ie.Scope = item.GetScope()
	ie.ConfigPath = item.GetConfigPath()
	ie.Metadata = item.GetMetadata()
}

// displayOS / displayArch / displayEcosystem turn a proto enum value
// into a short, human-friendly CLI string by stripping the long enum
// prefix and lower-casing the remainder. UNSPECIFIED returns "unknown".

func displayOS(v messagescontroltowerv1.EndpointOS) string {
	return prettyEnum(v.String(), "ENDPOINT_OS_")
}

func displayArch(v messagescontroltowerv1.EndpointArch) string {
	return prettyEnum(v.String(), "ENDPOINT_ARCH_")
}

func displayEcosystem(v packagev1.Ecosystem) string {
	return prettyEnum(v.String(), "ECOSYSTEM_")
}

func prettyEnum(name, prefix string) string {
	s := strings.TrimPrefix(name, prefix)
	if s == "" || s == "UNSPECIFIED" {
		return "unknown"
	}
	return strings.ToLower(s)
}
