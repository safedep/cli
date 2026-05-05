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
	"github.com/safedep/dry/log"
)

const (
	envProfile     = "SAFEDEP_PROFILE"
	defaultProfile = "default"

	// gRPCClientName identifies the CLI in server logs and request
	// metadata. It is NOT the keychain app name: dry/cloud's
	// DefaultAppName ("safedep") is shared across vet, pmg, and the CLI
	// so credentials saved by one tool are discoverable by the others
	// (per ADR Authentication section).
	gRPCClientName = "safedep-cli"
)

// App is constructed once in main(). Output and profile are populated by
// the root command's PersistentPreRunE before any leaf RunE fires.
type App struct {
	Config *config.Config
	Output *output.Output

	mu      sync.Mutex
	profile string

	credStore      cloud.CredentialStore
	apiKeyResolver cloud.CredentialResolver
	tokenResolver  cloud.CredentialResolver

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
// PersistentPreRunE with the value of --profile (which may be empty).
// Resolution order: flag, then env, then built-in default.
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

// KeychainOptions returns the dry/cloud options the auth flows must use
// when constructing stores or resolvers themselves. Only the profile is
// scoped. The keychain app name is left at dry/cloud's DefaultAppName
// ("safedep") so credentials saved here are visible to vet, pmg, and any
// other SafeDep tool that shares the same default.
func (a *App) KeychainOptions() []cloud.KeychainOption {
	return []cloud.KeychainOption{
		cloud.WithProfile(a.Profile()),
	}
}

// CredentialStore returns the keychain-backed credential store, scoped to
// the active profile. Initialised lazily.
func (a *App) CredentialStore() (cloud.CredentialStore, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.credStore != nil {
		return a.credStore, nil
	}

	store, err := cloud.NewKeychainCredentialStore(a.keychainOptsLocked()...)
	if err != nil {
		return nil, fmt.Errorf("app: credential store: %w", err)
	}

	a.credStore = store
	return store, nil
}

// APIKeyResolver returns the API-key credential resolver for the active
// profile. Initialised lazily. Env vars (SAFEDEP_API_KEY +
// SAFEDEP_TENANT_ID) win over the keychain, matching the convention
// shared with vet/pmg: CI/headless environments stay self-contained
// without needing an explicit `auth login`.
//
// A keychain construction failure (e.g. headless Linux with no DBus) is
// non-fatal here: we log it and fall back to an env-only chain so the
// documented headless/CI flow keeps working when the env vars are set.
// If neither env vars nor a keychain are usable, the error surfaces at
// Resolve time on the first DataPlane() call.
func (a *App) APIKeyResolver() (cloud.CredentialResolver, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.apiKeyResolver != nil {
		return a.apiKeyResolver, nil
	}

	envResolver, err := cloud.NewEnvCredentialResolver()
	if err != nil {
		return nil, fmt.Errorf("app: env resolver: %w", err)
	}

	resolvers := []cloud.CredentialResolver{envResolver}

	keychainResolver, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeAPIKey, a.keychainOptsLocked()...)
	if err != nil {
		log.Warnf("app: keychain unavailable for API-key resolver; falling back to env-only chain: %v", err)
	} else {
		resolvers = append(resolvers, keychainResolver)
	}

	a.apiKeyResolver = cloud.NewChainCredentialResolver(resolvers...)
	return a.apiKeyResolver, nil
}

// TokenResolver returns the OAuth-token credential resolver for the
// active profile. Initialised lazily.
func (a *App) TokenResolver() (cloud.CredentialResolver, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.tokenResolver != nil {
		return a.tokenResolver, nil
	}

	r, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeToken, a.keychainOptsLocked()...)
	if err != nil {
		return nil, fmt.Errorf("app: token resolver: %w", err)
	}

	a.tokenResolver = r
	return r, nil
}

// DataPlane returns the data plane client for the active profile,
// initialising it on first call. Returns a user-facing error when no
// credentials are available so commands can propagate it directly.
func (a *App) DataPlane() (*cloud.Client, error) {
	resolver, err := a.APIKeyResolver()
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
		return nil, errors.New("not authenticated: run `safedep auth login` first")
	}

	client, err := cloud.NewDataPlaneClient(gRPCClientName, creds)
	if err != nil {
		return nil, fmt.Errorf("app: data plane client: %w", err)
	}

	a.dataPlane = client
	return a.dataPlane, nil
}

// ControlPlane returns the control plane client for the active profile.
// Returns a user-facing error when no OAuth credentials are available.
// Auto-refresh of expired tokens is a future feature. Today an expired
// token surfaces as a clear "session expired" error.
func (a *App) ControlPlane() (*cloud.Client, error) {
	resolver, err := a.TokenResolver()
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.controlPlane != nil {
		return a.controlPlane, nil
	}

	creds, err := resolver.Resolve()
	if err != nil {
		return nil, errors.New("not authenticated for control plane: run `safedep auth login`")
	}

	client, err := cloud.NewControlPlaneClient(gRPCClientName, creds)
	if err != nil {
		return nil, fmt.Errorf("app: control plane client: %w", err)
	}

	a.controlPlane = client
	return a.controlPlane, nil
}

// Close releases resources held by lazily-initialised collaborators.
func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.credStore != nil {
		closeAndLog("credential store", a.credStore.Close)
	}

	closeResolverIfCloseable("api-key resolver", a.apiKeyResolver)
	closeResolverIfCloseable("token resolver", a.tokenResolver)

	if a.dataPlane != nil {
		closeAndLog("data plane client", a.dataPlane.Close)
	}

	if a.controlPlane != nil {
		closeAndLog("control plane client", a.controlPlane.Close)
	}
}

func (a *App) keychainOptsLocked() []cloud.KeychainOption {
	return []cloud.KeychainOption{
		cloud.WithProfile(a.profile),
	}
}

func closeResolverIfCloseable(label string, r cloud.CredentialResolver) {
	if r == nil {
		return
	}

	c, ok := r.(cloud.CloseableCredentialResolver)
	if !ok {
		return
	}

	closeAndLog(label, c.Close)
}

func closeAndLog(label string, closeFn func() error) {
	if err := closeFn(); err != nil {
		log.Warnf("app: close %s: %v", label, err)
	}
}
