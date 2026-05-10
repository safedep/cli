package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/dry/tui/table"
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
	cmd.Flags().DurationVar(&since, "since", 24*time.Hour, "trailing window length, e.g. 24h, 168h, 30m")
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
	t := table.New().Headers("Metric", "Value").Rows(
		[]string{"Total endpoints", fmt.Sprint(r.data.TotalEndpoints)},
		[]string{"Active", fmt.Sprint(r.data.ActiveEndpoints)},
		[]string{"Silent", fmt.Sprint(r.data.SilentEndpoints)},
		[]string{"Total events", fmt.Sprint(r.data.TotalEvents)},
		[]string{"PMG blocked", fmt.Sprint(r.data.PMGBlockedEvents)},
	)
	return t.Render()
}
