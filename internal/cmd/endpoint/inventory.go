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
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
)

type inventoryInput struct {
	Window      TimeWindow
	EndpointIDs []string
	Kinds       []messagescontroltowerv1.InventoryItemKind
	Scope       *messagescontroltowerv1.InventoryScope
	PageSize    uint32
	PageToken   string
}

// inventoryKindVocab maps kebab-case CLI flag values to proto enum values.
// Keep in lockstep with the InventoryItemKind enum.
var inventoryKindVocab = map[string]messagescontroltowerv1.InventoryItemKind{
	"mcp-server":        messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_MCP_SERVER,
	"coding-agent":      messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_CODING_AGENT,
	"ai-extension":      messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_AI_EXTENSION,
	"cli-tool":          messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_CLI_TOOL,
	"project-config":    messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_PROJECT_CONFIG,
	"browser-extension": messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_BROWSER_EXTENSION,
	"ide-extension":     messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_IDE_EXTENSION,
	"agent-plugin":      messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_AGENT_PLUGIN,
	"agent-skill":       messagescontroltowerv1.InventoryItemKind_INVENTORY_ITEM_KIND_AGENT_SKILL,
}

func mapKinds(values []string) ([]messagescontroltowerv1.InventoryItemKind, error) {
	out := make([]messagescontroltowerv1.InventoryItemKind, 0, len(values))
	for _, v := range values {
		k, ok := inventoryKindVocab[strings.ToLower(v)]
		if !ok {
			return nil, fmt.Errorf("unknown inventory kind %q (use one of: mcp-server|coding-agent|ai-extension|cli-tool|project-config|browser-extension|ide-extension|agent-plugin|agent-skill)", v)
		}
		out = append(out, k)
	}
	return out, nil
}

func mapScope(s string) (*messagescontroltowerv1.InventoryScope, error) {
	if s == "" {
		return nil, nil
	}
	var v messagescontroltowerv1.InventoryScope
	switch strings.ToLower(s) {
	case "system":
		v = messagescontroltowerv1.InventoryScope_INVENTORY_SCOPE_SYSTEM
	case "project":
		v = messagescontroltowerv1.InventoryScope_INVENTORY_SCOPE_PROJECT
	default:
		return nil, fmt.Errorf("unknown scope %q (use system|project)", s)
	}
	return &v, nil
}

// inventoryScopeLabel renders the InventoryScope enum as a CLI-friendly
// lowercase label, stripping the "INVENTORY_SCOPE_" prefix.
// UNSPECIFIED returns "unknown".
func inventoryScopeLabel(s messagescontroltowerv1.InventoryScope) string {
	raw := strings.TrimPrefix(s.String(), "INVENTORY_SCOPE_")
	if raw == "" || raw == "UNSPECIFIED" {
		return "unknown"
	}
	return strings.ToLower(raw)
}

func inventoryListCmd(a *app.App) *cobra.Command {
	var (
		scopeFlag, pageTokenFlag string
		since                    time.Duration
		endpoints, kindsRaw      []string
		pageSize                 uint32
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List current endpoint inventory",
		Long:  "List the current inventory snapshot for one or more endpoints. Inventory events are deduped client-side by item identity, keeping the most recent event per identity.",
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
			kinds, err := mapKinds(kindsRaw)
			if err != nil {
				return err
			}
			scope, err := mapScope(scopeFlag)
			if err != nil {
				return err
			}
			in := inventoryInput{
				Window:      window,
				EndpointIDs: ids,
				Kinds:       kinds,
				Scope:       scope,
				PageSize:    pageSize,
				PageToken:   pageTokenFlag,
			}
			res, err := runInventory(cmd.Context(), NewService(client.Connection()), dir, in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.DurationVar(&since, "since", 7*24*time.Hour, "trailing window length, e.g. 168h, 24h, 30m")
	f.StringSliceVar(&endpoints, "endpoint", nil, "filter by endpoint (ULID or cached hostname); repeatable")
	f.StringSliceVar(&kindsRaw, "kind", nil, "filter by inventory kind (mcp-server|coding-agent|ai-extension|cli-tool|project-config|browser-extension|ide-extension|agent-plugin|agent-skill); repeatable")
	f.StringVar(&scopeFlag, "scope", "", "filter by scope: system|project")
	f.Uint32Var(&pageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&pageTokenFlag, "page-token", "", "continuation token from a prior response")
	return cmd
}

func runInventory(ctx context.Context, svc InventoryEventLister, dir *Directory, in inventoryInput) (*inventoryResult, error) {
	res, err := svc.ListInventoryEvents(ctx, InventoryEventsInput{
		Window:      in.Window,
		EndpointIDs: in.EndpointIDs,
		ItemKinds:   in.Kinds,
		Scope:       in.Scope,
		PageSize:    in.PageSize,
		PageToken:   in.PageToken,
	})
	if err != nil {
		return nil, err
	}
	items := dedupeByItemIdentity(res.Events)
	if in.PageSize > 0 && len(res.Events) >= int(in.PageSize) {
		tui.Warning("inventory list page is full; dedupe is best-effort within this page. Pass --endpoint to narrow or --page-token to continue.")
	}
	ids := make([]string, 0, len(items))
	for _, e := range items {
		ids = append(ids, e.EndpointID)
	}
	return &inventoryResult{
		items:          items,
		nextPage:       res.NextPage,
		window:         in.Window,
		endpointLabels: resolveEndpointLabels(ctx, dir, ids),
	}, nil
}

func dedupeByItemIdentity(events []InventoryEvent) []InventoryEvent {
	latest := map[string]InventoryEvent{}
	for _, e := range events {
		cur, ok := latest[e.ItemIdentity]
		if !ok || e.Timestamp.After(cur.Timestamp) {
			latest[e.ItemIdentity] = e
		}
	}
	out := make([]InventoryEvent, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type inventoryResult struct {
	items          []InventoryEvent
	nextPage       string
	window         TimeWindow
	endpointLabels map[string]string
}

func (r *inventoryResult) windowLabel() string {
	if r.window.Start.IsZero() || r.window.End.IsZero() {
		return "server default"
	}
	return "last " + r.window.End.Sub(r.window.Start).Round(time.Minute).String()
}

type inventoryJSONItem struct {
	EndpointID string            `json:"endpoint_id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	App        string            `json:"app"`
	Scope      string            `json:"scope"`
	ConfigPath string            `json:"config_path,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	LastSeen   time.Time         `json:"last_seen"`
}

func (r *inventoryResult) RenderJSON() ([]byte, error) {
	items := make([]inventoryJSONItem, 0, len(r.items))
	for _, e := range r.items {
		items = append(items, inventoryJSONItem{
			EndpointID: e.EndpointID,
			Kind:       inventoryKindLabel(e.Kind),
			Name:       inventoryDisplayName(e),
			App:        e.App,
			Scope:      inventoryScopeLabel(e.Scope),
			ConfigPath: e.ConfigPath,
			Metadata:   e.Metadata,
			LastSeen:   e.Timestamp,
		})
	}
	out := struct {
		Items         []inventoryJSONItem `json:"items"`
		NextPageToken string              `json:"next_page_token,omitempty"`
	}{Items: items, NextPageToken: r.nextPage}
	return json.MarshalIndent(out, "", "  ")
}

func (r *inventoryResult) RenderPlain() string {
	if len(r.items) == 0 {
		return fmt.Sprintf("no inventory in %s. Try a wider window with --since.", r.windowLabel())
	}
	var b strings.Builder
	for _, e := range r.items {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n",
			endpointLabel(e.EndpointID, r.endpointLabels),
			inventoryKindLabel(e.Kind),
			inventoryDisplayName(e),
			e.App,
			inventoryScopeLabel(e.Scope),
			formatTime(e.Timestamp),
		)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *inventoryResult) RenderTable() string {
	if len(r.items) == 0 {
		return fmt.Sprintf("no inventory in %s. Try a wider window with --since.", r.windowLabel())
	}
	rows := make([][]string, 0, len(r.items))
	for _, e := range r.items {
		rows = append(rows, []string{
			endpointLabel(e.EndpointID, r.endpointLabels),
			inventoryKindLabel(e.Kind),
			inventoryDisplayName(e),
			e.App,
			inventoryScopeLabel(e.Scope),
			formatTime(e.Timestamp),
		})
	}
	return table.New().Headers("Endpoint", "Kind", "Name", "App", "Scope", "Last Seen").Rows(rows...).Render()
}
