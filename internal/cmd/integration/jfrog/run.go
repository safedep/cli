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

const (
	envJFrogURL   = "SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_URL"
	envJFrogToken = "SAFEDEP_INTEGRATION_JFROG_ARTIFACTORY_ACCESS_TOKEN"
)

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

// resolveConfig builds Config from flags with env var fallbacks.
// Flag values take precedence; env vars are the fallback.
func resolveConfig(in runInput) (Config, error) {
	url := in.InstanceURL
	if url == "" {
		url = config.EnvVar(envJFrogURL)
	}
	if url == "" {
		return Config{}, fmt.Errorf("run: --instance-url or %s is required", envJFrogURL)
	}
	// Ensure https.
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

	cursorFile := in.CursorFile
	if cursorFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("run: resolve cursor file home dir: %w", err)
		}
		cursorFile = filepath.Join(home, ".safedep", "integration-jfrog-cursor.json")
	} else if strings.HasPrefix(cursorFile, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("run: resolve cursor file home dir: %w", err)
		}
		cursorFile = filepath.Join(home, cursorFile[2:])
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
