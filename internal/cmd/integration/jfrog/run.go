// internal/cmd/integration/jfrog/run.go
package jfrog

import (
	"fmt"
	"os"
	"path/filepath"
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
	envJFrogURL   = "SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_URL"
	envJFrogToken = "SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_ACCESS_TOKEN"
)

// runInput is the raw, unresolved CLI input. Defaults and env-var fallbacks
// are applied later by resolveConfig so RunE stays free of business logic.
type runInput struct {
	InstanceURL         string
	InstanceAccessToken string
	PollInterval        time.Duration
	CursorFile          string
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

			svc := newFeedService(
				malysisv1grpc.NewMalwareAnalysisServiceClient(client.Connection()),
				cfg,
			)

			return svc.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&in.InstanceURL, "instance-url", "", "JFrog instance URL (or "+envJFrogURL+")")
	cmd.Flags().StringVar(&in.InstanceAccessToken, "instance-access-token", "", "JFrog access token (or "+envJFrogToken+")")
	cmd.Flags().DurationVar(&in.PollInterval, "poll-interval", 60*time.Second, "sleep duration between poll cycles")
	cmd.Flags().StringVar(&in.CursorFile, "cursor-file", "", "cursor file path (default ~/.safedep/integration-jfrog-cursor.json)")

	return cmd
}

// resolveConfig collapses CLI flags + environment variables into a single
// runtime Config. Resolution precedence (highest to lowest):
//
//  1. Explicit CLI flag value
//  2. Corresponding SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_* environment variable
//  3. Built-in default (only for poll interval and cursor file)
//
// Required parameters that cannot be defaulted (URL, access token) cause a
// hard error so the daemon fails fast at startup rather than running blind.
func resolveConfig(in runInput) (Config, error) {
	url := in.InstanceURL
	if url == "" {
		url = config.EnvVar(envJFrogURL)
	}
	if url == "" {
		return Config{}, fmt.Errorf("run: --instance-url or %s is required", envJFrogURL)
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
		return Config{}, fmt.Errorf("run: --instance-access-token or %s is required", envJFrogToken)
	}

	cursorFile, err := resolveCursorFile(in.CursorFile)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Source: SourceConfig{
			PollInterval: in.PollInterval,
			CursorFile:   cursorFile,
		},
		JFrog: JFrogConfig{
			URL:         url,
			AccessToken: token,
		},
	}, nil
}

// resolveCursorFile returns the absolute cursor path. An empty input picks
// the default under the user's home directory; a leading "~/" is expanded
// because filepath.Join does not (it is a shell-only convention).
func resolveCursorFile(path string) (string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("run: resolve cursor file home dir: %w", err)
		}
		return filepath.Join(home, ".safedep", "integration-jfrog-cursor.json"), nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("run: resolve cursor file home dir: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
