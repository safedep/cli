// internal/cmd/integration/jfrog/run.go
package jfrog

import (
	"fmt"
	"strings"
	"time"

	threatintelv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/threatintel/v1/threatintelv1grpc"
	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
)

// Environment variable names mirrored by their corresponding flags.
// Naming follows the SAFEDEP_<DOMAIN>_<NOUN>_<FIELD> convention so multiple
// integrations can coexist without collisions.
const (
	envJFrogURL = "SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_URL"
	// envJFrogToken is the variable NAME, not a credential. The actual token
	// is read at runtime via config.EnvVar.
	envJFrogToken = "SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_ACCESS_TOKEN" // #nosec G101

	// kvNamespace is the profile-scoped KV namespace for this integration.
	// Must match ^[a-z][a-z0-9_-]{0,63}$.
	kvNamespace = "integration-jfrog"

	// cursorKey is the single KV key used to store the feed cursor.
	kvCursorKey = "cursor"
)

// runInput is the raw, unresolved CLI input. Defaults and env-var fallbacks
// are applied later by resolveConfig so RunE stays free of business logic.
type runInput struct {
	InstanceURL         string
	InstanceAccessToken string
	PollInterval        time.Duration
	Backfill            time.Duration
	// DryRun previews the feed and sends nothing to JFrog.
	DryRun bool
}

func runCmd(a *app.App) *cobra.Command {
	var in runInput

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the JFrog XRay malicious package feed",
		Long:  "Stream verified malicious packages from the SafeDep ThreatIntel Feed and push them to JFrog XRay as Custom Issues.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.DataPlane()
			if err != nil {
				return err
			}

			cfg, err := resolveConfig(in)
			if err != nil {
				return err
			}

			svc := threatintelv1grpc.NewThreatIntelServiceClient(client.Connection())

			rep := newReporter(a.Output)
			source, xc, err := buildSourceAndClient(a, svc, cfg, rep)
			if err != nil {
				return err
			}

			return newFeedService(source, xc, rep).run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&in.InstanceURL, "instance-url", "", "JFrog instance URL (or "+envJFrogURL+")")
	// A token on the command line is saved in the shell history and shown in the
	// process list. So the flag name says insecure. Prefer the env var.
	cmd.Flags().StringVar(&in.InstanceAccessToken, "insecure-instance-access-token", "", "JFrog access token, insecure (prefer "+envJFrogToken+")")
	cmd.Flags().DurationVar(&in.PollInterval, "poll-interval", 5*time.Minute, "sleep duration between feed drains")
	cmd.Flags().DurationVar(&in.Backfill, "backfill", 0, "first-run window to seed the cursor (e.g. 24h, 168h); 0 starts fresh from now")
	cmd.Flags().BoolVar(&in.DryRun, "dry-run", false, "preview the feed and print what would be pushed, without sending to JFrog (no JFrog credentials needed)")

	return cmd
}

// buildSourceAndClient wires the feed source and XRay client port for the
// resolved config. Both modes share the same feed and the same persistent,
// profile-scoped cursor: a dry-run tests the pipeline as-is and differs only in
// the client (print instead of JFrog). A dry-run advances the saved cursor, so
// run `cursor remove` before the first real run to re-process what it previewed.
func buildSourceAndClient(a *app.App, svc threatintelv1grpc.ThreatIntelServiceClient, cfg cmdConfig, rep *reporter) (*feedSource, xrayClient, error) {
	// Cursor is stored in the profile-scoped KV store so each SafeDep
	// credential profile has an independent cursor. Switching --profile
	// automatically switches the cursor.
	kv, err := app.ProfileKV[cursorState](a, kvNamespace)
	if err != nil {
		return nil, nil, fmt.Errorf("run: open cursor store: %w", err)
	}

	source := newFeedSource(svc, kv, cfg.source.pollInterval, cfg.source.backfillWindow, rep)

	if cfg.dryRun {
		return source, newPrintClient(rep), nil
	}
	return source, newJFrogClient(cfg.jfrog, rep), nil
}

// resolveConfig collapses CLI flags + environment variables into a single
// runtime Config. Resolution precedence (highest to lowest):
//
//  1. Explicit CLI flag value
//  2. Corresponding SAFEDEP_INTEGRATION_JFROG_* environment variable
//  3. Built-in default (poll interval, backfill window)
//
// A dry-run sends nothing to JFrog, so the URL and access token are optional in
// that mode; a real run requires both and fails fast at startup if either is
// missing rather than running blind.
func resolveConfig(in runInput) (cmdConfig, error) {
	source, err := resolveSourceConfig(in)
	if err != nil {
		return cmdConfig{}, err
	}

	if in.DryRun {
		return cmdConfig{source: source, dryRun: true}, nil
	}

	jfrog, err := resolveJFrogConfig(in)
	if err != nil {
		return cmdConfig{}, err
	}

	return cmdConfig{source: source, jfrog: jfrog}, nil
}

// resolveSourceConfig resolves the SafeDep-side feed parameters. These are
// identical for a real run and a dry-run: a dry-run mirrors production, so the
// operator picks --backfill the same way in both modes.
func resolveSourceConfig(in runInput) (sourceConfig, error) {
	// time.After(<= 0) fires immediately. A zero or negative interval would
	// turn the feed loop into a tight infinite hammer on the SafeDep API
	// with no backoff. Refuse rather than silently DoS the upstream.
	if in.PollInterval <= 0 {
		return sourceConfig{}, fmt.Errorf("run: --poll-interval must be positive, got %s", in.PollInterval)
	}
	if in.Backfill < 0 {
		return sourceConfig{}, fmt.Errorf("run: --backfill must be >= 0, got %s", in.Backfill)
	}

	return sourceConfig{
		pollInterval:   in.PollInterval,
		backfillWindow: in.Backfill,
	}, nil
}

// resolveJFrogConfig resolves and validates the JFrog destination. Required for
// a real run only.
func resolveJFrogConfig(in runInput) (jfrogConfig, error) {
	url := in.InstanceURL
	if url == "" {
		url = config.EnvVar(envJFrogURL)
	}
	if url == "" {
		return jfrogConfig{}, fmt.Errorf("run: --instance-url or %s is required", envJFrogURL)
	}
	// Force https. JFrog XRay will accept tokens over plain HTTP, but doing
	// so leaks the bearer token over the wire. Better to silently upgrade
	// than to leave a footgun.
	if strings.HasPrefix(url, "http://") {
		url = "https://" + url[len("http://"):]
	} else if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	token := in.InstanceAccessToken
	if token == "" {
		token = config.EnvVar(envJFrogToken)
	}
	if token == "" {
		return jfrogConfig{}, fmt.Errorf("run: --insecure-instance-access-token or %s is required", envJFrogToken)
	}

	return jfrogConfig{url: url, accessToken: token}, nil
}
