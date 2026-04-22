package app

import (
	"fmt"

	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/output"
	"github.com/safedep/dry/cloud"
)

type App struct {
	Config *config.Config
	Output *output.Formatter

	dataPlane    *cloud.Client
	controlPlane *cloud.Client
	credStore    cloud.CredentialStore
	credResolver cloud.CredentialResolver
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
		Output:       output.New(format),
		credStore:    credStore,
		credResolver: credResolver,
	}, nil
}

// CredentialStore exposes the store so domain services can be constructed by the cmd layer.
func (a *App) CredentialStore() cloud.CredentialStore {
	return a.credStore
}

// CredentialResolver exposes the resolver so the cmd layer can resolve credentials.
func (a *App) CredentialResolver() cloud.CredentialResolver {
	return a.credResolver
}

// RequireDataPlane returns the data plane client, initialising it on first call.
func (a *App) RequireDataPlane() (*cloud.Client, error) {
	if a.dataPlane != nil {
		return a.dataPlane, nil
	}

	creds, err := a.credResolver.Resolve()
	if err != nil {
		return nil, fmt.Errorf("not authenticated — run `safedep auth login` first")
	}

	client, err := cloud.NewDataPlaneClient("safedep-cli", creds)
	if err != nil {
		return nil, fmt.Errorf("app: data plane client: %w", err)
	}

	a.dataPlane = client
	return a.dataPlane, nil
}

// RequireControlPlane returns an error until OAuth support lands.
func (a *App) RequireControlPlane() (*cloud.Client, error) {
	if a.controlPlane != nil {
		return a.controlPlane, nil
	}

	return nil, fmt.Errorf("this command requires OAuth login — not yet available, coming in a future release")
}

func (a *App) Close() {
	if a.credStore != nil {
		_ = a.credStore.Close()
	}
	if closer, ok := a.credResolver.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
