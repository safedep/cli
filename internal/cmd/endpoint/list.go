package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

type listInput struct {
	Window               TimeWindow
	Capabilities         []controltowerv1.EndpointCapability
	OnlyBlocked          bool
	SilentFor            time.Duration
	Search               string
	LabelSelector        *messagescontroltowerv1.LabelSelector
	AppliedRevisionBelow uint64
	PageSize             uint32
	PageToken            string
	_now                 func() time.Time // injectable for tests
}

type capabilityToken string

const (
	capabilityGuard     capabilityToken = "guard"
	capabilityTracer    capabilityToken = "tracer"
	capabilityAdvisor   capabilityToken = "advisor"
	capabilityInventory capabilityToken = "inventory"
)

func listCmd(a *app.App) *cobra.Command {
	var (
		searchFlag, pageTokenFlag string
		selectorFlag, appliedFlag string
		since                     time.Duration
		capabilitiesRaw           []string
		onlyBlocked               bool
		silentFor                 time.Duration
		pageSize                  uint32
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List endpoints with filters",
		Long: "List endpoints reporting to SafeDep Cloud, with filters for capability, blocked installs, silent duration, identity search, " +
			"labels, and the applied policy revision. --applied-revision '<48' keeps the endpoints where a tool applies a policy " +
			"revision lower than 48, so you can see which endpoints have not applied a publish yet. The POLICY column shows the " +
			"applied policy revision that each tool reported.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			selector, applied, err := parsePolicyFilters(selectorFlag, appliedFlag)
			if err != nil {
				return err
			}
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			caps, err := mapCapabilities(capabilitiesRaw)
			if err != nil {
				return err
			}
			window := WindowFromDuration(time.Now(), since)
			dir, err := NewDirectoryFromApp(a)
			if err != nil {
				return err
			}
			in := listInput{
				Window: window, Capabilities: caps,
				OnlyBlocked: onlyBlocked, SilentFor: silentFor, Search: searchFlag,
				LabelSelector: selector, AppliedRevisionBelow: applied,
				PageSize: pageSize, PageToken: pageTokenFlag,
			}
			res, err := runList(cmd.Context(), NewService(client.Connection()), dir, in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.DurationVar(&since, "since", 7*24*time.Hour, "trailing window length, e.g. 168h, 24h, 30m")
	f.StringSliceVar(&capabilitiesRaw, "capability", nil, "filter by capability (guard|tracer|advisor|inventory); repeatable")
	f.BoolVar(&onlyBlocked, "blocked", false, "only endpoints with at least one blocked install in the window")
	f.DurationVar(&silentFor, "silent-for", 0, "only endpoints not seen for at least this duration (client-side; best-effort within --limit)")
	f.StringVar(&searchFlag, "search", "", "case-insensitive substring match on hostname/name (client-side; best-effort within --limit)")
	f.StringVar(&selectorFlag, "selector", "", "filter: a label selector, as in managed/deployment-kind=laptop,custom/team=release")
	f.StringVar(&appliedFlag, "applied-revision", "", "filter: '<N' keeps endpoints where a tool applies a policy revision lower than N, as in '<48'")
	f.Uint32Var(&pageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&pageTokenFlag, "page-token", "", "continuation token from a prior response")
	return cmd
}

func runList(ctx context.Context, lister EndpointLister, dir *Directory, in listInput) (*listResult, error) {
	var minBlocked uint64
	if in.OnlyBlocked {
		minBlocked = 1
	}
	res, err := lister.List(ctx, ListInput{
		Window: in.Window, Capabilities: in.Capabilities, MinPMGBlocked: minBlocked,
		LabelSelector: in.LabelSelector, AppliedRevisionBelow: in.AppliedRevisionBelow,
		PageSize: in.PageSize, PageToken: in.PageToken,
	})
	if err != nil {
		return nil, err
	}

	now := in._now
	if now == nil {
		now = time.Now
	}
	eps := res.Endpoints
	if in.SilentFor > 0 {
		eps = filterSilent(eps, in.SilentFor, now())
	}
	if in.Search != "" {
		eps = filterSearch(eps, in.Search)
	}

	// Best-effort: populate the directory cache with what we just learned.
	upsert := make([]DirectoryEntry, 0, len(eps))
	for _, e := range eps {
		upsert = append(upsert, DirectoryEntry{ID: e.ID, Name: e.Identifier, Hostname: e.Hostname, LastSyncAt: e.LastSync})
	}
	_ = dir.Upsert(ctx, upsert)

	return &listResult{endpoints: eps, nextPage: res.NextPage}, nil
}

func filterSilent(eps []ListEndpoint, d time.Duration, now time.Time) []ListEndpoint {
	out := eps[:0]
	for _, e := range eps {
		// Zero LastSync means never synced. Treat as infinitely silent.
		if e.LastSync.IsZero() || now.Sub(e.LastSync) >= d {
			out = append(out, e)
		}
	}
	return out
}

func filterSearch(eps []ListEndpoint, q string) []ListEndpoint {
	q = strings.ToLower(q)
	out := eps[:0]
	for _, e := range eps {
		if strings.Contains(strings.ToLower(e.Hostname), q) || strings.Contains(strings.ToLower(e.Identifier), q) {
			out = append(out, e)
		}
	}
	return out
}

var capabilityVocab = map[capabilityToken]controltowerv1.EndpointCapability{
	capabilityGuard:     controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_PACKAGE_GUARD,
	capabilityTracer:    controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_TRACER,
	capabilityAdvisor:   controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_ADVISOR,
	capabilityInventory: controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_INVENTORY,
}

func mapCapabilities(values []string) ([]controltowerv1.EndpointCapability, error) {
	out := make([]controltowerv1.EndpointCapability, 0, len(values))
	for _, v := range values {
		tok, err := parseCapabilityToken(v)
		if err != nil {
			return nil, err
		}
		c, ok := capabilityVocab[tok]
		if !ok {
			return nil, fmt.Errorf("unknown capability %q (use guard|tracer|advisor|inventory)", v)
		}
		out = append(out, c)
	}
	return out, nil
}

func parseCapabilityToken(raw string) (capabilityToken, error) {
	tok := capabilityToken(strings.ToLower(strings.TrimSpace(raw)))
	if _, ok := capabilityVocab[tok]; !ok {
		return "", fmt.Errorf("unknown capability %q (use guard|tracer|advisor|inventory)", raw)
	}
	return tok, nil
}

type listResult struct {
	endpoints []ListEndpoint
	nextPage  string
}

func (r *listResult) RenderJSON() ([]byte, error) {
	type toolPolicy struct {
		Tool     string `json:"tool"`
		Version  string `json:"tool_version,omitempty"`
		Revision uint64 `json:"applied_policy_revision"`
		State    string `json:"state"`
	}
	type row struct {
		ID, Identifier, Hostname, OS, Arch string
		LastSync                           time.Time
		Capabilities                       []string
		Blocked                            uint64
		Inventory                          uint64
		ToolPolicies                       []toolPolicy `json:"tool_policies"`
	}
	out := struct {
		Endpoints []row  `json:"endpoints"`
		NextPage  string `json:"next_page_token,omitempty"`
	}{NextPage: r.nextPage}
	for _, e := range r.endpoints {
		policies := make([]toolPolicy, 0, len(e.ToolPolicies))
		for _, p := range e.ToolPolicies {
			policies = append(policies, toolPolicy{Tool: p.Tool, Version: p.Version, Revision: p.Revision, State: p.State})
		}
		out.Endpoints = append(out.Endpoints, row{
			ID: e.ID, Identifier: e.Identifier, Hostname: e.Hostname, OS: e.OS, Arch: e.Arch,
			LastSync: e.LastSync, Capabilities: capabilityNames(e.Capabilities),
			Blocked: e.PMGBlockedEvents, Inventory: e.InventoryEvents, ToolPolicies: policies,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *listResult) RenderPlain() string {
	if len(r.endpoints) == 0 {
		return "no endpoints"
	}
	var b strings.Builder
	b.WriteString("ID\tHOSTNAME\tOS\tCAPS\tLAST SYNC\tBLOCKED\tINVENTORY\tPOLICY\n")
	for _, e := range r.endpoints {
		b.WriteString(tui.PlainRow(e.ID, e.Hostname, e.OS+"/"+e.Arch, strings.Join(capabilityNames(e.Capabilities), ","),
			formatTime(e.LastSync), fmt.Sprint(e.PMGBlockedEvents), fmt.Sprint(e.InventoryEvents), policyCell(e.ToolPolicies)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *listResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.endpoints))
	for _, e := range r.endpoints {
		rows = append(rows, []string{
			shortID(e.ID), e.Hostname, e.OS + "/" + e.Arch,
			emptyDash(strings.Join(capabilityNames(e.Capabilities), ",")),
			humanize.Time(e.LastSync, now),
			fmt.Sprint(e.PMGBlockedEvents),
			fmt.Sprint(e.InventoryEvents),
			policyCell(e.ToolPolicies),
		})
	}
	t := table.New().
		Title("Endpoints").
		Headers("ID", "Hostname", "OS/Arch", "Capabilities", "Last Sync", "Blocked", "Inventory", "Policy").
		Rows(tui.EscapeCells(rows)...).
		EmptyMessage("No endpoints found. Endpoints appear after a SafeDep tool syncs. Try a wider --since window.")
	if len(rows) > 0 {
		footer := fmt.Sprintf("%d %s", len(rows), plural(len(rows), "endpoint", "endpoints"))
		if r.nextPage != "" {
			footer += fmt.Sprintf(". More available: --page-token %s", r.nextPage)
		}
		t = t.Footer(footer)
	}
	return t.Render()
}

// shortID returns a display-friendly prefix of an endpoint ULID. The
// prefix is itself a valid filter input: pass it to `endpoint show` or
// `--endpoint` and Resolve will match it as a ULID prefix.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// filterPackageDecisions drops PMG events that aren't package-decision
// events (e.g. session summaries), which arrive with empty Action and
// Package fields and would render as blank rows.
func filterPackageDecisions(events []GuardEvent) []GuardEvent {
	out := events[:0]
	for _, e := range events {
		if e.Action == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// verdictCell returns the user-facing verdict, optionally annotated
// with cooldown remaining when present. The annotation is suppressed
// when DaysRemaining looks unreasonable (e.g. uint32 wrap for packages
// already past their cooldown window) so the cell never shows nonsense.
func verdictCell(e GuardEvent) string {
	if e.Cooldown != nil && e.Verdict == VerdictCooldown {
		if r := e.Cooldown.DaysRemaining; r > 0 && r <= 365 {
			return fmt.Sprintf("cooldown (%dd remaining)", r)
		}
		return VerdictCooldown
	}
	return e.Verdict
}

// distinctInvocations counts non-empty distinct invocation IDs in the
// rows. Used to summarise how many tool runs produced a given set of
// events (e.g. "5 blocks across 3 tool runs").
func distinctInvocations(ids []string) int {
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
	}
	return len(seen)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// endpointLabel returns a human-friendly cell for an endpoint ID,
// preferring the cached hostname/identifier and falling back to a
// shortened ID when nothing is cached.
func endpointLabel(id string, labels map[string]string) string {
	if l, ok := labels[id]; ok && l != "" {
		return l
	}
	return shortID(id)
}

// resolveEndpointLabels walks unique endpoint IDs and asks the
// directory for a human label (hostname preferred, identifier
// fallback). Endpoints missing from the cache are simply omitted.
func resolveEndpointLabels(ctx context.Context, dir *Directory, ids []string) map[string]string {
	if dir == nil {
		return nil
	}
	out := map[string]string{}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		entry, ok := dir.Lookup(ctx, id)
		if !ok {
			continue
		}
		switch {
		case entry.Hostname != "":
			out[id] = entry.Hostname
		case entry.Name != "":
			out[id] = entry.Name
		}
	}
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04Z")
}

func capabilityNames(caps []controltowerv1.EndpointCapability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		switch c {
		case controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_PACKAGE_GUARD:
			out = append(out, "guard")
		case controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_TRACER:
			out = append(out, "tracer")
		case controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_ADVISOR:
			out = append(out, "advisor")
		case controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_INVENTORY:
			out = append(out, "inventory")
		default:
			out = append(out, c.String())
		}
	}
	return out
}

// parsePolicyFilters reads --selector and --applied-revision. The contract
// filters on a revision below a bound only, so '<N' is the one form.
func parsePolicyFilters(selector, applied string) (*messagescontroltowerv1.LabelSelector, uint64, error) {
	var sel *messagescontroltowerv1.LabelSelector
	if selector != "" {
		parsed, err := cloudpolicy.ParseSelector(selector)
		if err != nil {
			return nil, 0, err
		}
		sel = parsed
	}
	if applied == "" {
		return sel, 0, nil
	}
	text, ok := strings.CutPrefix(strings.TrimSpace(applied), "<")
	n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if !ok || err != nil || n < 2 {
		return nil, 0, fmt.Errorf("--applied-revision %q: write '<N' with N of 2 or more, as in '<48', "+
			"for the endpoints where a tool applies a policy revision lower than N", applied)
	}
	return sel, n, nil
}

// policyCell shows the applied policy revision of each tool, as "rev 47" when
// every tool applies the same revision, or "pmg 47, vet 46".
func policyCell(policies []ToolPolicy) string {
	if len(policies) == 0 {
		return "-"
	}
	same := true
	for _, p := range policies[1:] {
		if p.Revision != policies[0].Revision {
			same = false
		}
	}
	if same {
		return fmt.Sprintf("rev %d", policies[0].Revision)
	}
	parts := make([]string, 0, len(policies))
	for _, p := range policies {
		parts = append(parts, fmt.Sprintf("%s %d", p.Tool, p.Revision))
	}
	return strings.Join(parts, ", ")
}
