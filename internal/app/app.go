// Package app holds the dependency-injection container shared by all
// commands. It is intentionally thin: holders of long-lived collaborators
// (config, output, credentials, plane clients), no business logic.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	cliauth "github.com/safedep/cli/internal/auth"
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/cli/internal/storage"
	"github.com/safedep/cli/internal/tui"
	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/log"
)

const (
	envProfile     = "SAFEDEP_PROFILE"
	defaultProfile = "default"
)

// App is constructed once in main(). Output and profile are populated by
// the root command's PersistentPreRunE before any leaf RunE fires.
type App struct {
	Config *config.Config
	Output *tui.Printer

	mu                       sync.Mutex
	profile                  string
	insecureKeychainFallback bool

	credStore      cloud.CredentialStore
	apiKeyResolver cloud.CredentialResolver
	tokenResolver  cloud.CredentialResolver

	dataPlane    *cloud.Client
	controlPlane *cloud.Client

	storage storage.Storage
}

func New(cfg *config.Config) *App {
	return &App{
		Config:  cfg,
		Output:  tui.NewPrinter(tui.AutoMode()),
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

// SetInsecureKeychainFallback toggles the plaintext-file fallback for the
// keychain. Called by the root PersistentPreRunE with the value of
// --insecure-keychain-fallback. Must be set before the first credential
// store or resolver is constructed; flipping it later has no effect on
// already-cached collaborators.
func (a *App) SetInsecureKeychainFallback(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.insecureKeychainFallback = enabled
}

// KeychainOptions returns the dry/cloud options the auth flows must use
// when constructing stores or resolvers themselves. The profile is scoped
// and the insecure file fallback is enabled when the user opted in via
// --insecure-keychain-fallback. The keychain app name is left at
// dry/cloud's DefaultAppName ("safedep") so credentials saved here are
// visible to vet, pmg, and any other SafeDep tool that shares the same
// default.
func (a *App) KeychainOptions() []cloud.KeychainOption {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.keychainOptsLocked()
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

	client, err := cloud.NewDataPlaneClient(cliauth.GRPCAppName, creds)
	if err != nil {
		return nil, fmt.Errorf("app: data plane client: %w", err)
	}

	a.dataPlane = client
	return a.dataPlane, nil
}

// ControlPlane returns the control plane client for the active profile.
// If the stored access token is expired it attempts a silent refresh via the
// refresh token before building the client. On refresh failure the user is
// directed to re-authenticate.
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

	creds, err = a.refreshIfExpiredLocked(creds)
	if err != nil {
		return nil, err
	}

	client, err := cloud.NewControlPlaneClient(cliauth.GRPCAppName, creds)
	if err != nil {
		return nil, fmt.Errorf("app: control plane client: %w", err)
	}

	a.controlPlane = client
	return a.controlPlane, nil
}

// refreshIfExpiredLocked silently refreshes the access token when it is
// expired. It must be called with a.mu held. On success it returns new
// credentials backed by the freshly-saved keychain entry. On refresh
// failure it returns ErrRefreshFailed so the caller can prompt re-login.
func (a *App) refreshIfExpiredLocked(creds *cloud.Credentials) (*cloud.Credentials, error) {
	store, err := a.keychainStoreForRefreshLocked()
	if err != nil {
		return nil, fmt.Errorf("app: refresh: credential store: %w", err)
	}

	fresh, err := cliauth.RefreshAndPersistIfExpired(context.Background(), store, creds, a.keychainOptsLocked())
	if err != nil {
		return nil, err
	}

	if fresh != creds {
		// Token was refreshed; reset the resolver cache so future calls use the new token.
		a.tokenResolver = nil
	}

	return fresh, nil
}

// keychainStoreForRefreshLocked returns the credential store, constructing it
// if it has not been initialised yet. Must be called with a.mu held.
func (a *App) keychainStoreForRefreshLocked() (cloud.CredentialStore, error) {
	if a.credStore != nil {
		return a.credStore, nil
	}
	store, err := cloud.NewKeychainCredentialStore(a.keychainOptsLocked()...)
	if err != nil {
		return nil, err
	}
	a.credStore = store
	return store, nil
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

	if a.storage != nil {
		closeAndLog("storage", a.storage.Close)
	}
}

func (a *App) keychainOptsLocked() []cloud.KeychainOption {
	opts := []cloud.KeychainOption{
		cloud.WithProfile(a.profile),
	}
	if a.insecureKeychainFallback {
		opts = append(opts, cloud.WithInsecureFileFallback())
	}
	return opts
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
