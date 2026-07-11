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
	"github.com/safedep/dry/tui/humanize"
	"github.com/safedep/dry/tui/section"
	"github.com/safedep/dry/tui/table"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// CLI-facing activity-source vocabulary.
type activityType string

const (
	ActivityTypeAll       activityType = "all"
	ActivityTypeGuard     activityType = "guard"
	ActivityTypeInventory activityType = "inventory"
)

type activityInput struct {
	Type         activityType
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
		Long:  "List recent activity across endpoints (blocked installs and inventory detections), filterable by type, action, tool, endpoint, and time window.",
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
			resolvedType := typeFlag
			if !cmd.Flags().Changed("type") && len(actions) > 0 {
				resolvedType = string(ActivityTypeGuard)
			}
			typ, err := parseActivityType(resolvedType)
			if err != nil {
				return err
			}
			in := activityInput{
				Type: typ, Window: window, EndpointIDs: ids,
				Actions: actions, Tool: toolFlag, InvocationID: invFlag,
				PageSize: pageSize, PageToken: pageTokenFlag,
			}
			res, err := runActivity(cmd.Context(), NewService(client.Connection()), dir, in)
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	f := cmd.Flags()
	f.StringVar(&typeFlag, "type", string(ActivityTypeGuard), "activity type: all|guard|inventory")
	f.DurationVar(&since, "since", 7*24*time.Hour, "trailing window length, e.g. 24h, 168h, 30m")
	f.StringSliceVar(&endpoints, "endpoint", nil, "filter by endpoint (ULID or cached hostname); repeatable")
	f.StringSliceVar(&actionsRaw, "action", nil, "guard-only action filter: blocked|confirmed|trusted|cooldown-blocked. Setting this implies --type=guard unless --type is set explicitly.")
	f.StringVar(&toolFlag, "tool", "", "client-side filter by tool_name")
	f.StringVar(&invFlag, "invocation", "", "scope to a single tool run; pass an invocation_id from JSON output")
	f.Uint32Var(&pageSize, "limit", 0, "page size; server default when 0")
	f.StringVar(&pageTokenFlag, "page-token", "", "continuation token from a prior response")
	return cmd
}

type activityRow struct {
	Timestamp    time.Time `json:"time"`
	EndpointID   string    `json:"endpoint_id"`
	Type         string    `json:"type"`
	Tool         string    `json:"tool"`
	Summary      string    `json:"summary"`
	InvocationID string    `json:"invocation_id,omitempty"`
	Raw          any       `json:"raw,omitempty"`
}

type activityResult struct {
	rows           []activityRow
	nextPage       string // single-source pagination only; empty in "all" mode
	endpointLabels map[string]string
}

func runActivity(ctx context.Context, svc activitySvc, dir *Directory, in activityInput) (*activityResult, error) {
	typ := in.Type
	if typ == "" {
		typ = ActivityTypeGuard
	}
	if err := validateActivityType(typ); err != nil {
		return nil, err
	}

	actions := in.Actions
	if (typ == ActivityTypeGuard || typ == ActivityTypeAll) && len(actions) == 0 {
		actions = []GuardAction{ActionBlocked, ActionCooldownBlocked}
	}

	var (
		gr *GuardEventsResult
		ir *InventoryEventsResult
	)
	g, gctx := errgroup.WithContext(ctx)
	if typ == ActivityTypeGuard || typ == ActivityTypeAll {
		g.Go(func() error {
			res, err := svc.ListGuardEvents(gctx, GuardEventsInput{
				Window: in.Window, EndpointIDs: in.EndpointIDs,
				Actions: actions, InvocationID: in.InvocationID,
				PageSize: in.PageSize, PageToken: pageTokenFor(in.PageToken, ActivityTypeGuard, typ),
			})
			if err != nil {
				return err
			}
			gr = res
			return nil
		})
	}
	if typ == ActivityTypeInventory || typ == ActivityTypeAll {
		g.Go(func() error {
			res, err := svc.ListInventoryEvents(gctx, InventoryEventsInput{
				Window: in.Window, EndpointIDs: in.EndpointIDs,
				InvocationID: in.InvocationID,
				PageSize:     in.PageSize, PageToken: pageTokenFor(in.PageToken, ActivityTypeInventory, typ),
			})
			if err != nil {
				return err
			}
			ir = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var rows []activityRow
	var next string

	if gr != nil {
		for _, e := range filterPackageDecisions(gr.Events) {
			if in.Tool != "" && !strings.EqualFold(in.Tool, e.Tool) {
				continue
			}
			rows = append(rows, activityRow{
				Timestamp: e.Timestamp, EndpointID: e.EndpointID, Type: string(ActivityTypeGuard),
				Tool:         e.Tool,
				Summary:      guardSummary(e),
				InvocationID: e.InvocationID,
				Raw:          e.Raw,
			})
		}
		if typ == ActivityTypeGuard {
			next = gr.NextPage
		}
	}

	if ir != nil {
		for _, e := range ir.Events {
			if in.Tool != "" && !strings.EqualFold(in.Tool, e.Tool) {
				continue
			}
			rows = append(rows, activityRow{
				Timestamp: e.Timestamp, EndpointID: e.EndpointID, Type: string(ActivityTypeInventory),
				Tool:         e.Tool,
				Summary:      fmt.Sprintf("detected: %s (%s)", inventoryDisplayName(e), inventoryKindLabel(e.Kind)),
				InvocationID: e.InvocationID,
				Raw:          e.Raw,
			})
		}
		if typ == ActivityTypeInventory {
			next = ir.NextPage
		}
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Timestamp.After(rows[j].Timestamp) })

	if in.PageSize > 0 && len(rows) > int(in.PageSize) {
		rows = rows[:in.PageSize]
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.EndpointID)
	}
	return &activityResult{
		rows:           rows,
		nextPage:       next,
		endpointLabels: resolveEndpointLabels(ctx, dir, ids),
	}, nil
}

func parseActivityType(raw string) (activityType, error) {
	typ := activityType(strings.ToLower(strings.TrimSpace(raw)))
	if typ == "" {
		return ActivityTypeGuard, nil
	}
	if err := validateActivityType(typ); err != nil {
		return "", err
	}
	return typ, nil
}

func validateActivityType(typ activityType) error {
	switch typ {
	case ActivityTypeAll, ActivityTypeGuard, ActivityTypeInventory:
		return nil
	default:
		return fmt.Errorf("unknown activity type %q (use all|guard|inventory)", typ)
	}
}

func pageTokenFor(token string, source, typ activityType) string {
	if typ == source {
		return token
	}
	return ""
}

func guardSummary(e GuardEvent) string {
	label := string(e.Action)
	if e.Verdict != "" {
		label = verdictCell(e)
	}
	return fmt.Sprintf("%s: %s@%s (%s)", label, e.PackageName, e.PackageVersion, e.Ecosystem)
}

func inventoryDisplayName(e InventoryEvent) string {
	if e.Name != "" {
		return e.Name
	}
	return e.ItemIdentity
}

func inventoryKindLabel(k messagescontroltowerv1.InventoryItemKind) string {
	return strings.ReplaceAll(prettyEnum(k.String(), "INVENTORY_ITEM_KIND_"), "_", "-")
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
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n",
			formatTime(row.Timestamp), endpointLabel(row.EndpointID, r.endpointLabels), row.Type, row.Tool, row.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *activityResult) RenderTable() string {
	now := time.Now()
	rows := make([][]string, 0, len(r.rows))
	ids := make([]string, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, []string{humanize.Time(row.Timestamp, now), endpointLabel(row.EndpointID, r.endpointLabels), row.Type, row.Tool, row.Summary})
		ids = append(ids, row.InvocationID)
	}
	t := table.New().
		Title("Endpoint activity").
		Headers("Time", "Endpoint", "Type", "Tool", "Summary").
		Rows(rows...).
		EmptyMessage("No activity found. Try a wider --since window or fewer filters.")
	if len(rows) == 0 {
		return t.Render()
	}
	runs := distinctInvocations(ids)
	t = t.Footer(fmt.Sprintf("%d %s across %d tool %s",
		len(rows), plural(len(rows), "event", "events"),
		runs, plural(runs, "run", "runs")))
	return section.Join(t.Render(),
		section.Hint("Use --output json for invocation_id, drill in with --invocation <id>."))
}
