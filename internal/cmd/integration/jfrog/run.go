// internal/cmd/integration/jfrog/run.go
package jfrog

import (
	malysisv1grpc "buf.build/gen/go/safedep/api/grpc/go/safedep/services/malysis/v1/malysisv1grpc"
	"github.com/safedep/cli/internal/app"
	"github.com/spf13/cobra"
)

type runInput struct {
	ConfigPath string
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

			cfg, err := loadConfig(in.ConfigPath)
			if err != nil {
				return err
			}

			svc := newFeedService(
				malysisv1grpc.NewMalwareAnalysisServiceClient(client.Connection()),
				*cfg,
			)

			return svc.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVarP(&in.ConfigPath, "config", "c", "", "path to config file (required)")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}
