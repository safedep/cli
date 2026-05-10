package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

type activityInput struct {
	Type         string // "all", "guard", "inventory"
	Window       TimeWindow
	EndpointIDs  []string
	Actions      []GuardAction
	Tool         string
	InvocationID string
	PageSize     uint32
	PageToken    string
}

type activitySvc interface {
	GuardEventLister
	InventoryEventLister
}

func activityListCmd(a *app.App) *cobra.Command {
	var (
		typeFlag, toolFlag, invFlag, pageTokenFlag string
		since                                      time.Duration
		endpoints, actionsRaw                      []string
		pageSize                                   uint32
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent endpoint activity",
		Long:  "List recent activity across endpoints (blocked installs and inventory detections), filterable by type, action, kind, tool, endpoint, and time window.",
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
			ids := make([]string, 0, len(endpoints))
			for _, ref := range endpoints {
				id, err := dir.Resolve(cmd.Context(), ref)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			actions := make([]GuardAction, len(actionsRaw))
			for i, a := range actionsRaw {
				actions[i] = GuardAction(strings.ToLower(a))
			}
			in := activityInput{
				Type: typeFlag, Window: window, EndpointIDs: ids,
				Actions: actions, Tool: toolFlag, InvocationID: invFlag,
				PageSize: pageSize, PageToken: pageTokenFlag,
			}
			res, err := runActivity(cmd.Context(), NewService(client.Connection()), in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&typeFlag, "type", "all", "activity type: all|guard|inventory")
	f.DurationVar(&since, "since", 24*time.Hour, "trailing window length, e.g. 24h, 168h, 30m")
	f.StringSliceVar(&endpoints, "endpoint", nil, "filter by endpoint (ULID or cached hostname); repeatable")
	f.StringSliceVar(&actionsRaw, "action", nil, "package action filter for guard events: blocked|confirmed|trusted|cooldown-blocked (default: blocked when type includes guard)")
	f.StringVar(&toolFlag, "tool", "", "client-side filter by tool_name")
	f.StringVar(&invFlag, "invocation", "", "scope to a specific invocation id")
	f.Uint32Var(&pageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&pageTokenFlag, "page-token", "", "continuation token from a prior response")
	return cmd
}

type activityRow struct {
	Timestamp  time.Time `json:"time"`
	EndpointID string    `json:"endpoint_id"`
	Type       string    `json:"type"`
	Tool       string    `json:"tool"`
	Summary    string    `json:"summary"`
	Raw        any       `json:"raw,omitempty"`
}

type activityResult struct {
	rows     []activityRow
	nextPage string // single-source pagination only; empty in "all" mode
}

func runActivity(ctx context.Context, svc activitySvc, in activityInput) (*activityResult, error) {
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ == "" {
		typ = "all"
	}

	actions := in.Actions
	if (typ == "guard" || typ == "all") && len(actions) == 0 {
		actions = []GuardAction{"blocked"}
	}

	var rows []activityRow
	var next string

	if typ == "guard" || typ == "all" {
		gr, err := svc.ListGuardEvents(ctx, GuardEventsInput{
			Window: in.Window, EndpointIDs: in.EndpointIDs,
			Actions: actions, InvocationID: in.InvocationID,
			PageSize: in.PageSize, PageToken: pageTokenFor(in.PageToken, "guard", typ),
		})
		if err != nil {
			return nil, err
		}
		for _, e := range gr.Events {
			if in.Tool != "" && !strings.EqualFold(in.Tool, e.Tool) {
				continue
			}
			rows = append(rows, activityRow{
				Timestamp: e.Timestamp, EndpointID: e.EndpointID, Type: "guard",
				Tool:    e.Tool,
				Summary: fmt.Sprintf("%s: %s@%s (%s)", e.Action, e.PackageName, e.PackageVersion, e.Ecosystem),
				Raw:     e.Raw,
			})
		}
		if typ == "guard" {
			next = gr.NextPage
		}
	}

	if typ == "inventory" || typ == "all" {
		ir, err := svc.ListInventoryEvents(ctx, InventoryEventsInput{
			Window: in.Window, EndpointIDs: in.EndpointIDs,
			InvocationID: in.InvocationID,
			PageSize: in.PageSize, PageToken: pageTokenFor(in.PageToken, "inventory", typ),
		})
		if err != nil {
			return nil, err
		}
		for _, e := range ir.Events {
			if in.Tool != "" && !strings.EqualFold(in.Tool, e.Tool) {
				continue
			}
			rows = append(rows, activityRow{
				Timestamp: e.Timestamp, EndpointID: e.EndpointID, Type: "inventory",
				Tool:    e.Tool,
				Summary: fmt.Sprintf("detected: %s (%s)", inventoryDisplayName(e), inventoryKindLabel(e.Kind)),
				Raw:     e.Raw,
			})
		}
		if typ == "inventory" {
			next = ir.NextPage
		}
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Timestamp.After(rows[j].Timestamp) })

	if in.PageSize > 0 && len(rows) > int(in.PageSize) {
		rows = rows[:in.PageSize]
	}

	return &activityResult{rows: rows, nextPage: next}, nil
}

// pageTokenFor returns the page token to forward to a single source. In
// single-source mode the user-supplied token is forwarded as-is. In "all"
// mode pagination is approximate and tokens are not threaded back; v2
// will introduce compound tokens.
func pageTokenFor(token, source, typ string) string {
	if typ == source {
		return token
	}
	return ""
}

// inventoryDisplayName returns a human-readable label for an inventory event.
// Falls back to ItemIdentity when Name is empty.
func inventoryDisplayName(e InventoryEvent) string {
	if e.Name != "" {
		return e.Name
	}
	return e.ItemIdentity
}

// inventoryKindLabel renders the InventoryItemKind enum as a CLI-friendly
// kebab-case label, stripping the "INVENTORY_ITEM_KIND_" prefix and
// mapping UNSPECIFIED to "unknown".
func inventoryKindLabel(k messagescontroltowerv1.InventoryItemKind) string {
	s := strings.TrimPrefix(k.String(), "INVENTORY_ITEM_KIND_")
	if s == "" || s == "UNSPECIFIED" {
		return "unknown"
	}
	return strings.ReplaceAll(strings.ToLower(s), "_", "-")
}

func (r *activityResult) RenderJSON() ([]byte, error) {
	out := struct {
		Rows     []activityRow `json:"rows"`
		NextPage string        `json:"next_page_token,omitempty"`
	}{Rows: r.rows, NextPage: r.nextPage}
	return json.MarshalIndent(out, "", "  ")
}

func (r *activityResult) RenderPlain() string {
	if len(r.rows) == 0 {
		return "no activity"
	}
	var b strings.Builder
	for _, row := range r.rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n", formatTime(row.Timestamp), shortID(row.EndpointID), row.Type, row.Tool, row.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *activityResult) RenderTable() string {
	if len(r.rows) == 0 {
		return "no activity"
	}
	rows := make([][]string, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, []string{formatTime(row.Timestamp), shortID(row.EndpointID), row.Type, row.Tool, row.Summary})
	}
	return table.New().Headers("Time", "Endpoint", "Type", "Tool", "Summary").Rows(rows...).Render()
}
