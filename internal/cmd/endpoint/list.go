package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	controltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

type listInput struct {
	Window       TimeWindow
	Capabilities []string
	OnlyBlocked  bool
	SilentFor    time.Duration
	Search       string
	PageSize     uint32
	PageToken    string
	_now         func() time.Time // injectable for tests
}

func listCmd(a *app.App) *cobra.Command {
	var (
		searchFlag, pageTokenFlag string
		since                     time.Duration
		capabilities              []string
		onlyBlocked               bool
		silentFor                 time.Duration
		pageSize                  uint32
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List endpoints with filters",
		Long:  "List endpoints reporting to SafeDep Cloud, with filters for capability, blocked installs, silent duration, and identity search.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			window := WindowFromDuration(time.Now(), since)
			dir, err := NewDirectoryFromApp(a)
			if err != nil {
				return err
			}
			in := listInput{
				Window: window, Capabilities: capabilities,
				OnlyBlocked: onlyBlocked, SilentFor: silentFor, Search: searchFlag,
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
	f.StringSliceVar(&capabilities, "capability", nil, "filter by capability (guard|tracer|advisor|inventory); repeatable")
	f.BoolVar(&onlyBlocked, "blocked", false, "only endpoints with at least one blocked install in the window")
	f.DurationVar(&silentFor, "silent-for", 0, "only endpoints not seen for at least this duration (client-side; best-effort within --limit)")
	f.StringVar(&searchFlag, "search", "", "case-insensitive substring match on hostname/name (client-side; best-effort within --limit)")
	f.Uint32Var(&pageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&pageTokenFlag, "page-token", "", "continuation token from a prior response")
	return cmd
}

func runList(ctx context.Context, lister EndpointLister, dir *Directory, in listInput) (*listResult, error) {
	caps, err := mapCapabilities(in.Capabilities)
	if err != nil {
		return nil, err
	}
	var minBlocked uint64
	if in.OnlyBlocked {
		minBlocked = 1
	}
	res, err := lister.List(ctx, ListInput{
		Window: in.Window, Capabilities: caps, MinPMGBlocked: minBlocked,
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
		// Zero LastSync means never synced; treat as infinitely silent.
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

var capabilityVocab = map[string]controltowerv1.EndpointCapability{
	"guard":     controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_PACKAGE_GUARD,
	"tracer":    controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_TRACER,
	"advisor":   controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_ADVISOR,
	"inventory": controltowerv1.EndpointCapability_ENDPOINT_CAPABILITY_INVENTORY,
}

func mapCapabilities(values []string) ([]controltowerv1.EndpointCapability, error) {
	out := make([]controltowerv1.EndpointCapability, 0, len(values))
	for _, v := range values {
		c, ok := capabilityVocab[strings.ToLower(v)]
		if !ok {
			return nil, fmt.Errorf("unknown capability %q (use guard|tracer|advisor|inventory)", v)
		}
		out = append(out, c)
	}
	return out, nil
}

type listResult struct {
	endpoints []ListEndpoint
	nextPage  string
}

func (r *listResult) RenderJSON() ([]byte, error) {
	type row struct {
		ID, Identifier, Hostname, OS, Arch string
		LastSync                           time.Time
		Capabilities                       []string
		Blocked                            uint64
		Inventory                          uint64
	}
	out := struct {
		Endpoints []row  `json:"endpoints"`
		NextPage  string `json:"next_page_token,omitempty"`
	}{NextPage: r.nextPage}
	for _, e := range r.endpoints {
		out.Endpoints = append(out.Endpoints, row{
			ID: e.ID, Identifier: e.Identifier, Hostname: e.Hostname, OS: e.OS, Arch: e.Arch,
			LastSync: e.LastSync, Capabilities: capabilityNames(e.Capabilities),
			Blocked: e.PMGBlockedEvents, Inventory: e.InventoryEvents,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func (r *listResult) RenderPlain() string {
	if len(r.endpoints) == 0 {
		return "no endpoints"
	}
	var b strings.Builder
	b.WriteString("ID\tHOSTNAME\tOS\tCAPS\tLAST SYNC\tBLOCKED\tINVENTORY\n")
	for _, e := range r.endpoints {
		fmt.Fprintf(&b, "%s\t%s\t%s/%s\t%s\t%s\t%d\t%d\n",
			e.ID, e.Hostname, e.OS, e.Arch, strings.Join(capabilityNames(e.Capabilities), ","),
			formatTime(e.LastSync), e.PMGBlockedEvents, e.InventoryEvents,
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *listResult) RenderTable() string {
	if len(r.endpoints) == 0 {
		return "no endpoints"
	}
	rows := make([][]string, 0, len(r.endpoints))
	for _, e := range r.endpoints {
		rows = append(rows, []string{
			shortID(e.ID), e.Hostname, e.OS + "/" + e.Arch,
			strings.Join(capabilityNames(e.Capabilities), ","),
			formatTime(e.LastSync),
			fmt.Sprint(e.PMGBlockedEvents),
			fmt.Sprint(e.InventoryEvents),
		})
	}
	return table.New().Headers("ID", "Hostname", "OS/Arch", "Capabilities", "Last Sync", "Blocked", "Inventory").Rows(rows...).Render()
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
