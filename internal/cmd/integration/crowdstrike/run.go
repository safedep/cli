package crowdstrike

import (
	"fmt"
	"time"

	controltowerv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/controltower/v1/controltowerv1grpc"
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

const (
	// kvNamespace is the profile-scoped KV namespace for this integration.
	// Must match ^[a-z][a-z0-9_-]{0,63}$.
	kvNamespace = "integration-crowdstrike"

	// kvCursorKey is the single KV key used to store the sync cursor.
	kvCursorKey = "cursor"
)

// runInput is the raw, unresolved CLI input. Defaults are applied by
// resolveConfig so RunE stays free of business logic.
type runInput struct {
	PollInterval time.Duration
	Backfill     time.Duration
}

func runCmd(a *app.App) *cobra.Command {
	var in runInput

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the CrowdStrike endpoint block event sync",
		Long: `Stream malicious-package and dependency-cooldown block events from SafeDep
endpoint telemetry and log them.

The events are pulled incrementally by cursor so each run resumes where it
stopped. A future stage will push them to CrowdStrike SIEM; for now they are
logged (use -o json for machine-readable output).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// EndpointManagementService is a control-plane service
			// (cloud.safedep.io, OAuth), the same client the `endpoint` commands
			// use. The data plane (api.safedep.io, API key) does not serve it.
			client, err := a.ControlPlane()
			if err != nil {
				return err
			}

			cfg, err := resolveConfig(in)
			if err != nil {
				return err
			}

			svc := controltowerv1grpc.NewEndpointManagementServiceClient(client.Connection())

			rep := newReporter(a.Output)
			source, sink, err := buildSourceAndSink(a, svc, cfg, rep)
			if err != nil {
				return err
			}

			return newEventService(source, sink, rep).run(cmd.Context())
		},
	}

	cmd.Flags().DurationVar(&in.PollInterval, "poll-interval", 5*time.Minute, "sleep duration between sync cycles")
	cmd.Flags().DurationVar(&in.Backfill, "backfill", 0, "first-run window to seed the cursor (e.g. 24h, 168h); 0 starts fresh from now")

	return cmd
}

// buildSourceAndSink wires the event source and the sink. Stage 1 always uses
// the print (logging) sink; Stage 2 will select the CrowdStrike sink here, the
// only wiring change needed to start pushing.
func buildSourceAndSink(a *app.App, svc controltowerv1grpc.EndpointManagementServiceClient, cfg cmdConfig, rep *reporter) (*eventSource, eventSink, error) {
	// Cursor is stored in the profile-scoped KV store so each SafeDep credential
	// profile has an independent cursor. Switching --profile switches the cursor.
	kv, err := app.ProfileKV[cursorState](a, kvNamespace)
	if err != nil {
		return nil, nil, fmt.Errorf("run: open cursor store: %w", err)
	}

	source := newEventSource(svc, kv, cfg.source.pollInterval, cfg.source.backfillWindow, rep)
	return source, newPrintClient(rep), nil
}

// resolveConfig collapses CLI flags into a runtime config, applying defaults and
// rejecting invalid values fast.
func resolveConfig(in runInput) (cmdConfig, error) {
	// time.After(<= 0) fires immediately, turning the loop into a tight hammer on
	// the SafeDep API with no backoff. Refuse rather than silently DoS upstream.
	if in.PollInterval <= 0 {
		return cmdConfig{}, fmt.Errorf("run: --poll-interval must be positive, got %s", in.PollInterval)
	}
	if in.Backfill < 0 {
		return cmdConfig{}, fmt.Errorf("run: --backfill must be >= 0, got %s", in.Backfill)
	}

	return cmdConfig{source: sourceConfig{
		pollInterval:   in.PollInterval,
		backfillWindow: in.Backfill,
	}}, nil
}
