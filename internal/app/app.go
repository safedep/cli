// Package app holds the dependency-injection container shared by all
// commands. It is intentionally thin: holders of long-lived collaborators
// (config, output, credentials, plane clients), no business logic.
package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/output"
	"github.com/safedep/dry/cloud"
)

const (
	envProfile     = "SAFEDEP_PROFILE"
	defaultProfile = "default"
	appName        = "safedep-cli"
)

// App is constructed once in main(). Output and profile are populated by
// the root command's PersistentPreRunE before any leaf RunE fires.
type App struct {
	Config *config.Config
	Output *output.Output

	mu      sync.Mutex
	profile string

	credStore    cloud.CredentialStore
	credResolver cloud.CredentialResolver

	dataPlane    *cloud.Client
	controlPlane *cloud.Client
}

func New(cfg *config.Config) *App {
	return &App{
		Config:  cfg,
		Output:  output.New(output.AutoMode()),
		profile: defaultProfile,
	}
}

// SetProfile records the active credential profile. Called by the root
// PersistentPreRunE with the value of --profile (which may be empty). The
// resolution order is: flag → env → built-in default.
func (a *App) SetProfile(flagValue string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if v := strings.TrimSpace(flagValue); v != "" {
		a.profile = v
		return
	}
	if v := strings.TrimSpace(os.Getenv(envProfile)); v != "" {
		a.profile = v
		return
	}
	a.profile = defaultProfile
}

// Profile returns the active credential profile name.
func (a *App) Profile() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.profile
}

// CredentialStore returns the keychain-backed credential store, scoped to
// the active profile. The store is initialised lazily so commands that do
// not touch credentials (e.g. --help) don't pay the cost.
func (a *App) CredentialStore() (cloud.CredentialStore, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.credStore != nil {
		return a.credStore, nil
	}

	store, err := cloud.NewKeychainCredentialStore(
		cloud.WithAppName(appName),
		cloud.WithProfile(a.profile),
	)
	if err != nil {
		return nil, fmt.Errorf("app: credential store: %w", err)
	}
	a.credStore = store
	return store, nil
}

// CredentialResolver returns the API-key credential resolver for the active
// profile. Initialised lazily.
func (a *App) CredentialResolver() (cloud.CredentialResolver, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.credResolver != nil {
		return a.credResolver, nil
	}

	resolver, err := cloud.NewKeychainCredentialResolver(
		cloud.CredentialTypeAPIKey,
		cloud.WithAppName(appName),
		cloud.WithProfile(a.profile),
	)
	if err != nil {
		return nil, fmt.Errorf("app: credential resolver: %w", err)
	}
	a.credResolver = resolver
	return resolver, nil
}

// RequireDataPlane returns the data plane client for the active profile,
// initialising it on first call.
func (a *App) RequireDataPlane() (*cloud.Client, error) {
	resolver, err := a.CredentialResolver()
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dataPlane != nil {
		return a.dataPlane, nil
	}

	creds, err := resolver.Resolve()
	if err != nil {
		return nil, errors.New("not authenticated — run `safedep auth login` first")
	}

	client, err := cloud.NewDataPlaneClient(appName, creds)
	if err != nil {
		return nil, fmt.Errorf("app: data plane client: %w", err)
	}

	a.dataPlane = client
	return a.dataPlane, nil
}

// RequireControlPlane returns the control plane client. Until OAuth lands
// it returns a clear, user-facing error.
func (a *App) RequireControlPlane() (*cloud.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.controlPlane != nil {
		return a.controlPlane, nil
	}
	return nil, errors.New("this command requires OAuth login — not yet available, coming in a future release")
}

// Close releases resources held by lazily-initialised collaborators.
func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.credStore != nil {
		_ = a.credStore.Close()
	}
	if closer, ok := a.credResolver.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
