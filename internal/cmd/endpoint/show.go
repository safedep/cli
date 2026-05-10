package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/log"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

type showInput struct {
	Ref    string
	Window TimeWindow
}

// showSvc is the union of interfaces show.go needs from a Service.
type showSvc interface {
	EndpointGetter
	GuardEventLister
	InventoryEventLister
}

func showCmd(a *app.App) *cobra.Command {
	var since time.Duration
	cmd := &cobra.Command{
		Use:   "show <endpoint>",
		Short: "Show endpoint detail",
		Long:  "Show identity, last sync, per-tool event volumes, last invocation, recent blocks, and current inventory size for one endpoint. Accepts a ULID or a cached hostname/identifier.",
		Args:  cobra.ExactArgs(1),
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
			res, err := runShow(cmd.Context(), NewService(client.Connection()), dir, showInput{Ref: args[0], Window: window})
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().DurationVar(&since, "since", 7*24*time.Hour, "trailing window length for per-tool counts and recent blocks, e.g. 168h, 24h")
	return cmd
}

type showResult struct {
	endpoint       *GetResult
	recentBlocks   []GuardEvent
	inventoryCount int
	window         TimeWindow
}

func runShow(ctx context.Context, svc showSvc, dir *Directory, in showInput) (*showResult, error) {
	id, err := dir.Resolve(ctx, in.Ref)
	if err != nil {
		return nil, err
	}
	ep, err := svc.Get(ctx, GetInput{EndpointID: id, Window: in.Window})
	if err != nil {
		return nil, err
	}

	res := &showResult{endpoint: ep, window: in.Window}

	blocks, err := svc.ListGuardEvents(ctx, GuardEventsInput{
		Window: in.Window, EndpointIDs: []string{id},
		Actions: []GuardAction{"blocked"}, PageSize: 5,
	})
	if err != nil {
		log.Warnf("endpoint show: recent blocks unavailable: %v", err)
	} else {
		res.recentBlocks = blocks.Events
	}

	invWindow := TimeWindow{Start: time.Now().Add(-24 * time.Hour), End: time.Now()}
	inv, err := svc.ListInventoryEvents(ctx, InventoryEventsInput{
		Window: invWindow, EndpointIDs: []string{id}, PageSize: 100,
	})
	if err != nil {
		log.Warnf("endpoint show: inventory peek unavailable: %v", err)
	} else {
		res.inventoryCount = countDistinctIdentities(inv.Events)
	}

	_ = dir.Upsert(ctx, []DirectoryEntry{{
		ID: ep.ID, Name: ep.Identifier, Hostname: ep.Hostname, LastSyncAt: ep.LastSync,
	}})

	return res, nil
}

func countDistinctIdentities(events []InventoryEvent) int {
	seen := make(map[string]struct{}, len(events))
	for _, e := range events {
		seen[e.ItemIdentity] = struct{}{}
	}
	return len(seen)
}

func (r *showResult) RenderJSON() ([]byte, error) {
	type block struct {
		Time      time.Time `json:"time"`
		Action    string    `json:"action"`
		Package   string    `json:"package"`
		Version   string    `json:"version"`
		Ecosystem string    `json:"ecosystem,omitempty"`
	}
	blocks := make([]block, 0, len(r.recentBlocks))
	for _, b := range r.recentBlocks {
		blocks = append(blocks, block{
			Time: b.Timestamp, Action: string(b.Action),
			Package: b.PackageName, Version: b.PackageVersion, Ecosystem: b.Ecosystem,
		})
	}
	out := struct {
		Endpoint       *GetResult `json:"endpoint"`
		Window         struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
		} `json:"window"`
		RecentBlocks   []block `json:"recent_blocks"`
		InventoryCount int     `json:"inventory_count"`
	}{
		Endpoint:       r.endpoint,
		RecentBlocks:   blocks,
		InventoryCount: r.inventoryCount,
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
	for _, blk := range r.recentBlocks {
		fmt.Fprintf(&b, "block\t%s\t%s\t%s\n", formatTime(blk.Timestamp), blk.PackageName, blk.PackageVersion)
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
		[]string{"Inventory items (24h)", fmt.Sprint(r.inventoryCount)},
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
			[]string{"CI context", boolYes(inv.HasCI)},
			[]string{"Agent context", boolYes(inv.HasAgent)},
		).Render())
	}

	if len(r.recentBlocks) > 0 {
		rows := make([][]string, 0, len(r.recentBlocks))
		for _, b := range r.recentBlocks {
			rows = append(rows, []string{formatTime(b.Timestamp), b.PackageName, b.PackageVersion, b.Ecosystem})
		}
		sections = append(sections, fmt.Sprintf("Recent blocks (%s):\n", r.windowLabel())+table.New().Headers("Time", "Package", "Version", "Ecosystem").Rows(rows...).Render())
	}

	return strings.Join(sections, "\n\n")
}

func boolYes(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// windowLabel renders the active TimeWindow as a short human label like
// "last 168h" so render output makes the applied --since explicit and
// users don't have to guess why a tighter window returns fewer rows.
func (r *showResult) windowLabel() string {
	if r.window.Start.IsZero() || r.window.End.IsZero() {
		return "server default"
	}
	d := r.window.End.Sub(r.window.Start).Round(time.Minute)
	return "last " + d.String()
}
