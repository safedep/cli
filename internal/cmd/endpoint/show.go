package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/log"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const (
	defaultGuardPageSize     uint32 = 50
	defaultInventoryPageSize uint32 = 100
)

type showInput struct {
	Ref             string
	Window          TimeWindow
	Blocks          uint32
	Actions         []GuardAction // empty -> default blocked + cooldown-blocked
	ShowInventory   bool
	InventoryKinds  []messagescontroltowerv1.InventoryItemKind
	InventoryLimit  uint32
}

// showSvc is the union of interfaces show.go needs from a Service.
type showSvc interface {
	EndpointGetter
	GuardEventLister
	InventoryEventLister
}

func showCmd(a *app.App) *cobra.Command {
	var (
		since          time.Duration
		blocks         uint32
		actionsRaw     []string
		inventoryFlag  bool
		invKinds       []string
		invLimit       uint32
	)
	cmd := &cobra.Command{
		Use:   "show <endpoint>",
		Short: "Show endpoint detail",
		Long:  "Show identity, last sync, per-tool event volumes, last invocation, recent guard events, and inventory for one endpoint. Accepts a ULID or a cached hostname/identifier.",
		Args:  showArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			window := WindowFromDuration(time.Now(), since)
			dir, err := NewDirectoryFromApp(a)
			if err != nil {
				return err
			}
			actions, err := parseShowActions(actionsRaw)
			if err != nil {
				return err
			}
			kinds, err := mapKinds(invKinds)
			if err != nil {
				return err
			}
			res, err := runShow(cmd.Context(), NewService(client.Connection()), dir, showInput{
				Ref: args[0], Window: window, Blocks: blocks,
				Actions:        actions,
				ShowInventory:  inventoryFlag,
				InventoryKinds: kinds,
				InventoryLimit: invLimit,
			})
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.DurationVar(&since, "since", 7*24*time.Hour, "trailing window length for per-tool counts and guard events, e.g. 168h, 24h")
	f.Uint32Var(&blocks, "blocks", defaultGuardPageSize, "max guard events to fetch in the window (server caps page size)")
	f.StringSliceVar(&actionsRaw, "actions", nil, "guard event actions to render: blocked|cooldown-blocked|confirmed|trusted|all (default: blocked,cooldown-blocked)")
	f.BoolVar(&inventoryFlag, "inventory", false, "render the inventory item list, not just the count")
	f.StringSliceVar(&invKinds, "inventory-kind", nil, "filter inventory list by kind (mcp-server|coding-agent|ai-extension|cli-tool|project-config|browser-extension|ide-extension|agent-plugin|agent-skill); repeatable")
	f.Uint32Var(&invLimit, "inventory-limit", defaultInventoryPageSize, "max inventory events to fetch in the window")
	return cmd
}

func showArgs(_ *cobra.Command, args []string) error {
	if len(args) == 1 {
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("missing endpoint: pass a ULID or cached hostname: example: safedep endpoint show <ENDPOINT-ID>. Discover IDs with: safedep endpoint list")
	}
	return fmt.Errorf("accepts exactly 1 endpoint argument: received %d", len(args))
}

// parseShowActions translates the --actions flag into the GuardAction
// slice passed to ListGuardEvents. Empty input falls back to the
// security-relevant defaults. "all" returns nil so the server returns
// every action type.
func parseShowActions(raw []string) ([]GuardAction, error) {
	if len(raw) == 0 {
		return []GuardAction{ActionBlocked, ActionCooldownBlocked}, nil
	}
	out := make([]GuardAction, 0, len(raw))
	for _, v := range raw {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "all" {
			return nil, nil
		}
		switch GuardAction(v) {
		case ActionBlocked, ActionCooldownBlocked, ActionConfirmed, ActionTrusted:
			out = append(out, GuardAction(v))
		default:
			return nil, fmt.Errorf("unknown action %q (use blocked|cooldown-blocked|confirmed|trusted|all)", v)
		}
	}
	return out, nil
}

type showResult struct {
	endpoint       *GetResult
	guardEvents    []GuardEvent
	guardActions   []GuardAction // nil means "all"
	inventoryCount int
	inventoryItems []InventoryEvent // populated only when --inventory was passed
	window         TimeWindow
}

func runShow(ctx context.Context, svc showSvc, dir *Directory, in showInput) (*showResult, error) {
	id, err := dir.Resolve(ctx, in.Ref)
	if err != nil {
		return nil, err
	}

	blockPage := in.Blocks
	if blockPage == 0 {
		blockPage = defaultGuardPageSize
	}
	invLimit := in.InventoryLimit
	if invLimit == 0 {
		invLimit = defaultInventoryPageSize
	}

	var (
		ep       *GetResult
		guard    *GuardEventsResult
		guardErr error
		inv      *InventoryEventsResult
		invErr   error
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		ep, err = svc.Get(gctx, GetInput{EndpointID: id, Window: in.Window})
		return err
	})
	g.Go(func() error {
		guard, guardErr = svc.ListGuardEvents(gctx, GuardEventsInput{
			Window: in.Window, EndpointIDs: []string{id},
			Actions: in.Actions, PageSize: blockPage,
		})
		return nil
	})
	g.Go(func() error {
		inv, invErr = svc.ListInventoryEvents(gctx, InventoryEventsInput{
			Window: in.Window, EndpointIDs: []string{id},
			ItemKinds: in.InventoryKinds, PageSize: invLimit,
		})
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	res := &showResult{endpoint: ep, window: in.Window, guardActions: in.Actions}

	if guardErr != nil {
		log.Warnf("endpoint show: guard events unavailable: %v", guardErr)
	} else if guard != nil {
		res.guardEvents = filterPackageDecisions(guard.Events)
	}

	if invErr != nil {
		log.Warnf("endpoint show: inventory peek unavailable: %v", invErr)
	} else if inv != nil {
		items := dedupeByItemIdentity(inv.Events)
		res.inventoryCount = len(items)
		if in.ShowInventory {
			res.inventoryItems = items
		}
	}

	_ = dir.Upsert(ctx, []DirectoryEntry{{
		ID: ep.ID, Name: ep.Identifier, Hostname: ep.Hostname, LastSyncAt: ep.LastSync,
	}})

	return res, nil
}

// guardSectionTitle labels the guard-events table based on the active
// action filter so users see what filter produced the rows.
func (r *showResult) guardSectionTitle() string {
	if len(r.guardActions) == 0 {
		return fmt.Sprintf("Recent guard events (all actions, %s)", r.windowLabel())
	}
	parts := make([]string, len(r.guardActions))
	for i, a := range r.guardActions {
		parts[i] = string(a)
	}
	return fmt.Sprintf("Recent guard events (%s, %s)", strings.Join(parts, ","), r.windowLabel())
}

func (r *showResult) RenderJSON() ([]byte, error) {
	type guardOut struct {
		Time         time.Time      `json:"time"`
		Verdict      string         `json:"verdict"`
		Action       string         `json:"action"`
		Package      string         `json:"package"`
		Version      string         `json:"version"`
		Ecosystem    string         `json:"ecosystem,omitempty"`
		InvocationID string         `json:"invocation_id,omitempty"`
		Cooldown     *GuardCooldown `json:"cooldown,omitempty"`
	}
	guardEvents := make([]guardOut, 0, len(r.guardEvents))
	for _, b := range r.guardEvents {
		guardEvents = append(guardEvents, guardOut{
			Time: b.Timestamp, Verdict: b.Verdict, Action: string(b.Action),
			Package: b.PackageName, Version: b.PackageVersion, Ecosystem: b.Ecosystem,
			InvocationID: b.InvocationID,
			Cooldown:     b.Cooldown,
		})
	}
	type invOut struct {
		Kind       string            `json:"kind"`
		Name       string            `json:"name"`
		App        string            `json:"app,omitempty"`
		Scope      string            `json:"scope,omitempty"`
		ConfigPath string            `json:"config_path,omitempty"`
		Metadata   map[string]string `json:"metadata,omitempty"`
		LastSeen   time.Time         `json:"last_seen"`
	}
	var invItems []invOut
	if r.inventoryItems != nil {
		invItems = make([]invOut, 0, len(r.inventoryItems))
		for _, e := range r.inventoryItems {
			invItems = append(invItems, invOut{
				Kind: inventoryKindLabel(e.Kind), Name: inventoryDisplayName(e),
				App: e.App, Scope: inventoryScopeLabel(e.Scope), ConfigPath: e.ConfigPath,
				Metadata: e.Metadata, LastSeen: e.Timestamp,
			})
		}
	}
	out := struct {
		Endpoint       *GetResult `json:"endpoint"`
		Window         struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
		} `json:"window"`
		GuardEvents    []guardOut `json:"guard_events"`
		InventoryCount int        `json:"inventory_count"`
		Inventory      []invOut   `json:"inventory,omitempty"`
	}{
		Endpoint: r.endpoint, GuardEvents: guardEvents,
		InventoryCount: r.inventoryCount, Inventory: invItems,
	}
	out.Window.Start = r.window.Start
	out.Window.End = r.window.End
	return json.MarshalIndent(out, "", "  ")
}

func (r *showResult) RenderPlain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "id\t%s\nhostname\t%s\nos\t%s/%s\nlast_sync\t%s\nwindow\t%s\ninventory_count\t%d\n",
		r.endpoint.ID, r.endpoint.Hostname, r.endpoint.OS, r.endpoint.Arch,
		formatTime(r.endpoint.LastSync), r.windowLabel(), r.inventoryCount)
	for _, v := range r.endpoint.PerToolVolumes {
		fmt.Fprintf(&b, "tool\t%s\t%d\n", v.Tool, v.Count)
	}
	for _, blk := range r.guardEvents {
		fmt.Fprintf(&b, "guard\t%s\t%s\t%s\t%s\t%s\n",
			formatTime(blk.Timestamp), verdictCell(blk), string(blk.Action), blk.PackageName, blk.PackageVersion)
	}
	for _, it := range r.inventoryItems {
		fmt.Fprintf(&b, "inv\t%s\t%s\t%s\t%s\t%s\n",
			inventoryKindLabel(it.Kind), inventoryDisplayName(it), it.App, inventoryScopeLabel(it.Scope), formatTime(it.Timestamp))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *showResult) RenderTable() string {
	var sections []string

	header := table.New().Headers("Field", "Value").Rows(
		[]string{"ID", r.endpoint.ID},
		[]string{"Hostname", r.endpoint.Hostname},
		[]string{"Identifier", r.endpoint.Identifier},
		[]string{"OS/Arch", r.endpoint.OS + "/" + r.endpoint.Arch},
		[]string{"Last Sync", formatTime(r.endpoint.LastSync)},
		[]string{fmt.Sprintf("Inventory items (%s)", r.windowLabel()), fmt.Sprint(r.inventoryCount)},
	).Render()
	sections = append(sections, header)

	if len(r.endpoint.PerToolVolumes) > 0 {
		rows := make([][]string, 0, len(r.endpoint.PerToolVolumes))
		for _, v := range r.endpoint.PerToolVolumes {
			rows = append(rows, []string{v.Tool, fmt.Sprint(v.Count)})
		}
		sections = append(sections, fmt.Sprintf("Per-tool events (%s):\n", r.windowLabel())+table.New().Headers("Tool", "Events").Rows(rows...).Render())
	}

	if r.endpoint.LastInvocation != nil {
		inv := r.endpoint.LastInvocation
		sections = append(sections, "Last invocation:\n"+table.New().Headers("Field", "Value").Rows(
			[]string{"Command", inv.Command},
			[]string{"Working dir", inv.WorkingDir},
			[]string{"CI context", yesNo(inv.HasCI)},
			[]string{"Agent context", yesNo(inv.HasAgent)},
		).Render())
	}

	if len(r.guardEvents) > 0 {
		rows := make([][]string, 0, len(r.guardEvents))
		ids := make([]string, 0, len(r.guardEvents))
		for _, b := range r.guardEvents {
			rows = append(rows, []string{formatTime(b.Timestamp), verdictCell(b), string(b.Action), b.PackageName, b.PackageVersion, b.Ecosystem})
			ids = append(ids, b.InvocationID)
		}
		runs := distinctInvocations(ids)
		section := r.guardSectionTitle() + ":\n" +
			table.New().Headers("Time", "Verdict", "Action", "Package", "Version", "Ecosystem").Rows(rows...).Render() +
			fmt.Sprintf("\n%d %s across %d tool %s. Use --output json for invocation_id then drill in with `safedep endpoint activity list --invocation <id>`.",
				len(rows), plural(len(rows), "event", "events"),
				runs, plural(runs, "run", "runs"))
		sections = append(sections, section)
	}

	if len(r.inventoryItems) > 0 {
		rows := make([][]string, 0, len(r.inventoryItems))
		for _, e := range r.inventoryItems {
			rows = append(rows, []string{
				inventoryKindLabel(e.Kind), inventoryDisplayName(e),
				e.App, inventoryScopeLabel(e.Scope), formatTime(e.Timestamp),
			})
		}
		section := fmt.Sprintf("Inventory (%s):\n", r.windowLabel()) +
			table.New().Headers("Kind", "Name", "App", "Scope", "Last Seen").Rows(rows...).Render() +
			fmt.Sprintf("\n%d distinct inventory %s.", len(rows), plural(len(rows), "item", "items"))
		sections = append(sections, section)
	}

	return strings.Join(sections, "\n\n")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (r *showResult) windowLabel() string { return r.window.Label() }
