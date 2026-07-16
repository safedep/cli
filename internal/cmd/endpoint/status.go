package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui/stat"
	"github.com/safedep/dry/tui/theme"
	"github.com/spf13/cobra"
)

type statusInput struct {
	Window TimeWindow
}

func statusCmd(a *app.App) *cobra.Command {
	var since time.Duration
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show fleet health",
		Long:  "Show tenant-wide endpoint health: total, active, silent, and blocked-install counts in a time window.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}
			window := WindowFromDuration(time.Now(), since)
			res, err := runStatus(cmd.Context(), NewService(client.Connection()), statusInput{Window: window})
			if err != nil {
				return err
			}
			return a.Output.Print(res)
		},
	}
	cmd.Flags().DurationVar(&since, "since", 7*24*time.Hour, "trailing window length, e.g. 168h, 24h, 30m")
	return cmd
}

func runStatus(ctx context.Context, fetcher StatsFetcher, in statusInput) (*statusResult, error) {
	res, err := fetcher.Stats(ctx, StatsInput(in))
	if err != nil {
		return nil, err
	}
	return &statusResult{data: res}, nil
}

type statusResult struct{ data *StatsResult }

func (r *statusResult) RenderJSON() ([]byte, error) {
	return json.MarshalIndent(r.data, "", "  ")
}

func (r *statusResult) RenderPlain() string {
	return fmt.Sprintf(
		"total\t%d\nactive\t%d\nsilent\t%d\nevents\t%d\nblocked\t%d",
		r.data.TotalEndpoints, r.data.ActiveEndpoints, r.data.SilentEndpoints,
		r.data.TotalEvents, r.data.PMGBlockedEvents,
	)
}

func (r *statusResult) RenderTable() string {
	return stat.Render(
		stat.Card{Label: "Total endpoints", Value: fmt.Sprint(r.data.TotalEndpoints)},
		stat.Card{Label: "Active", Value: fmt.Sprint(r.data.ActiveEndpoints), Accent: accentWhen(r.data.ActiveEndpoints > 0, theme.RoleSuccess)},
		stat.Card{Label: "Silent", Value: fmt.Sprint(r.data.SilentEndpoints), Accent: accentWhen(r.data.SilentEndpoints > 0, theme.RoleWarning)},
		stat.Card{Label: "Total events", Value: fmt.Sprint(r.data.TotalEvents)},
		stat.Card{Label: "PMG blocked", Value: fmt.Sprint(r.data.PMGBlockedEvents), Accent: accentWhen(r.data.PMGBlockedEvents > 0, theme.RoleError)},
	)
}

// accentWhen colors a stat value only when the metric is noteworthy, so a
// zero count does not shout.
func accentWhen(cond bool, role theme.Role) *theme.Role {
	if !cond {
		return nil
	}
	return &role
}
