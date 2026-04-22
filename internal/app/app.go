package app

import (
	"fmt"

	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/output"
	"github.com/safedep/dry/cloud"
)

type App struct {
	Config       *config.Config
	DataPlane    *cloud.Client
	ControlPlane *cloud.Client
	CredStore    cloud.CredentialStore
	CredResolver cloud.CredentialResolver
	Output       *output.Formatter
}

func New(cfg *config.Config, format output.Format) (*App, error) {
	credStore, err := cloud.NewKeychainCredentialStore(
		cloud.WithInsecureFileFallback(),
	)
	if err != nil {
		return nil, fmt.Errorf("app: credential store: %w", err)
	}

	credResolver, err := cloud.NewKeychainCredentialResolver(
		cloud.CredentialTypeAPIKey,
		cloud.WithInsecureFileFallback(),
	)
	if err != nil {
		_ = credStore.Close()
		return nil, fmt.Errorf("app: credential resolver: %w", err)
	}

	return &App{
		Config:       cfg,
		CredStore:    credStore,
		CredResolver: credResolver,
		Output:       output.New(format),
	}, nil
}

// RequireDataPlane returns the data plane client, initialising it on first
// call. Returns a clear error if no credentials are stored.
func (a *App) RequireDataPlane() (*cloud.Client, error) {
	if a.DataPlane != nil {
		return a.DataPlane, nil
	}

	creds, err := a.CredResolver.Resolve()
	if err != nil {
		return nil, fmt.Errorf("not authenticated — run `safedep auth login` first")
	}

	client, err := cloud.NewDataPlaneClient("safedep-cli", creds)
	if err != nil {
		return nil, fmt.Errorf("app: data plane client: %w", err)
	}

	a.DataPlane = client
	return a.DataPlane, nil
}

// RequireControlPlane returns an error until OAuth support lands.
func (a *App) RequireControlPlane() (*cloud.Client, error) {
	if a.ControlPlane != nil {
		return a.ControlPlane, nil
	}

	return nil, fmt.Errorf("this command requires OAuth login — not yet available, coming in a future release")
}

func (a *App) Close() {
	if a.CredStore != nil {
		_ = a.CredStore.Close()
	}
	if closer, ok := a.CredResolver.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
