// internal/cmd/integration/jfrog/run.go
package jfrog

import (
	"fmt"
	"strings"
	"time"

	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
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

	// cursorKey is the single KV key used to store the poll cursor.
	kvCursorKey = "cursor"
)

// runInput is the raw, unresolved CLI input. Defaults and env-var fallbacks
// are applied later by resolveConfig so RunE stays free of business logic.
type runInput struct {
	InstanceURL         string
	InstanceAccessToken string
	PollInterval        time.Duration
}

func runCmd(a *app.App) *cobra.Command {
	var in runInput

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the JFrog XRay malicious package feed",
		Long:  "Poll SafeDep for verified malicious packages and push them to JFrog XRay as Custom Issues.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.DataPlane()
			if err != nil {
				return err
			}

			cfg, err := resolveConfig(in)
			if err != nil {
				return err
			}

			// Cursor is stored in the profile-scoped KV store so each
			// SafeDep credential profile has an independent cursor.
			// Switching --profile automatically switches the cursor.
			kv, err := app.ProfileKV[cursorState](a, kvNamespace)
			if err != nil {
				return fmt.Errorf("run: open cursor store: %w", err)
			}

			source := newPollSource(
				malysisv1grpc.NewMalwareAnalysisServiceClient(client.Connection()),
				kv,
				cfg.source.pollInterval,
			)
			jc := newJFrogClient(cfg.jfrog)

			return newFeedService(source, jc).run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&in.InstanceURL, "instance-url", "", "JFrog instance URL (or "+envJFrogURL+")")
	cmd.Flags().StringVar(&in.InstanceAccessToken, "instance-access-token", "", "JFrog access token (or "+envJFrogToken+")")
	cmd.Flags().DurationVar(&in.PollInterval, "poll-interval", 60*time.Second, "sleep duration between poll cycles")

	return cmd
}

// resolveConfig collapses CLI flags + environment variables into a single
// runtime Config. Resolution precedence (highest to lowest):
//
//  1. Explicit CLI flag value
//  2. Corresponding SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_* environment variable
//  3. Built-in default (only for poll interval)
//
// Required parameters that cannot be defaulted (URL, access token) cause a
// hard error so the daemon fails fast at startup rather than running blind.
func resolveConfig(in runInput) (cmdConfig, error) {
	url := in.InstanceURL
	if url == "" {
		url = config.EnvVar(envJFrogURL)
	}
	if url == "" {
		return cmdConfig{}, fmt.Errorf("run: --instance-url or %s is required", envJFrogURL)
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
		return cmdConfig{}, fmt.Errorf("run: --instance-access-token or %s is required", envJFrogToken)
	}

	// time.After(<= 0) fires immediately. A zero or negative interval would
	// turn the poll loop into a tight infinite hammer on the SafeDep API
	// with no backoff — refuse rather than silently DoS the upstream.
	if in.PollInterval <= 0 {
		return cmdConfig{}, fmt.Errorf("run: --poll-interval must be positive, got %s", in.PollInterval)
	}

	return cmdConfig{
		source: sourceConfig{
			pollInterval: in.PollInterval,
		},
		jfrog: jfrogConfig{
			url:         url,
			accessToken: token,
		},
	}, nil
}
