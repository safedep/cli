package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/safedep/cli/internal/app"
	cliauth "github.com/safedep/cli/internal/auth"
	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/log"
	"github.com/safedep/dry/tui"
	"github.com/spf13/cobra"
)

type loginFlags struct {
	apiKey           bool
	tenant           string
	apiKeyValue      string
	apiKeyFromStdin  bool
	apiKeyExpiryDays int
	rotateAPIKey     bool
	noAPIKey         bool
}

func loginCmd(a *app.App) *cobra.Command {
	var flags loginFlags

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with SafeDep Cloud",
		Long: "Authenticate with SafeDep Cloud and store credentials in the keychain under the active profile. " +
			"Defaults to OAuth2 device-code login (browser-based); pass --api-key to bring an existing API key instead.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.apiKey {
				return runAPIKeyLogin(cmd, a, flags)
			}

			return runDeviceLogin(cmd, a, flags)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&flags.apiKey, "api-key", false, "use static API-key login instead of OAuth2 device flow")
	f.StringVar(&flags.tenant, "tenant", "", "SafeDep tenant domain (used as fallback when none is stored)")
	f.StringVar(&flags.apiKeyValue, "api-key-value", "", "API key value (only with --api-key; prefer --from-stdin or env)")
	f.BoolVar(&flags.apiKeyFromStdin, "from-stdin", false, "read API key from stdin (only with --api-key)")
	f.IntVar(&flags.apiKeyExpiryDays, "api-key-expiry-days", cliauth.DefaultAPIKeyExpiryDays, "expiry for API keys created during device login")
	f.BoolVar(&flags.rotateAPIKey, "rotate-api-key", false, "force creation of a new API key during device login")
	f.BoolVar(&flags.noAPIKey, "no-api-key", false, "skip API key creation during device login")

	return cmd
}

func runAPIKeyLogin(cmd *cobra.Command, a *app.App, flags loginFlags) error {
	apiKey, err := resolveAPIKey(flags)
	if err != nil {
		return err
	}

	tenant, err := resolveTenant(cmd, a, flags.tenant)
	if err != nil {
		return err
	}

	in := cliauth.APIKeyInput{APIKey: apiKey, Tenant: tenant}

	tui.Info("Verifying API key against the data plane…")
	if err := cliauth.VerifyAPIKey(cmd.Context(), in); err != nil {
		return fmt.Errorf("api key verification failed: %w", err)
	}

	store, err := a.CredentialStore()
	if err != nil {
		return err
	}

	if err := cliauth.SaveAPIKey(cmd.Context(), store, in); err != nil {
		return err
	}

	tui.Success("API key saved for tenant %q (profile %q).", tenant, a.Profile())
	return nil
}

func runDeviceLogin(cmd *cobra.Command, a *app.App, flags loginFlags) error {
	tui.Info("Starting OAuth2 device login…")

	res, err := cliauth.RunDeviceFlow(cmd.Context(), cliauth.PrintVerification,
		cliauth.EmailVerificationRetry(cmd.InOrStdin()))
	if err != nil {
		return err
	}

	preferredTenant := flags.tenant
	if preferredTenant == "" {
		preferredTenant = tenantFromResolver(a.TokenResolver)
		if preferredTenant == "" {
			preferredTenant = tenantFromResolver(a.APIKeyResolver)
		}
	}

	createKey := !flags.noAPIKey
	var name string
	var decideAPIKey func(context.Context, string) cliauth.APIKeyDecision
	if createKey {
		name = cliauth.APIKeyName(cliauth.Hostname(), time.Now())
		if !flags.rotateAPIKey {
			decideAPIKey = storedAPIKeyDecider(a)
		}
	}

	var registrationPrompter func() (*cliauth.RegistrationInput, error)
	if flags.noAPIKey {
		registrationPrompter = func() (*cliauth.RegistrationInput, error) {
			return nil, errors.New("--no-api-key cannot be used during initial registration: an API key is required to complete setup")
		}
	} else {
		registrationPrompter = cliauth.NewRegistrationPrompter(res.AccessToken)
	}

	bootstrap, err := cliauth.PostOAuthBootstrap(cmd.Context(), cliauth.BootstrapInput{
		AccessToken:          res.AccessToken,
		PreferredTenant:      preferredTenant,
		CreateAPIKey:         createKey,
		APIKeyName:           name,
		APIKeyExpiryDays:     flags.apiKeyExpiryDays,
		DecideAPIKey:         decideAPIKey,
		Picker:               cliauth.PromptTenantPicker,
		RegistrationPrompter: registrationPrompter,
	})
	if err != nil {
		return err
	}

	store, err := a.CredentialStore()
	if err != nil {
		return err
	}

	if err := cliauth.SaveBootstrapResult(store, res.AccessToken, res.RefreshToken, bootstrap); err != nil {
		return err
	}

	decision := bootstrap.APIKeyDecision
	if decision.Warning != "" {
		tui.Warning("%s", decision.Warning)
	}

	switch {
	case bootstrap.APIKey != "":
		if decision.Reason != "" {
			tui.Info("Created a new API key: %s.", decision.Reason)
		}
		tui.Success("Authenticated as %s (profile %q). API key created, expires %s.",
			bootstrap.Tenant, a.Profile(), bootstrap.APIKeyExpiresAt.UTC().Format("2006-01-02"))
	case decision.Keep:
		tui.Success("Authenticated as %s (profile %q). Kept the stored API key, expires %s.",
			bootstrap.Tenant, a.Profile(), decision.ExpiresAt.UTC().Format("2006-01-02"))
	case createKey:
		tui.Success("Authenticated as %s (profile %q). API key creation skipped — none in response.", bootstrap.Tenant, a.Profile())
	default:
		tui.Success("Authenticated as %s (profile %q). OAuth tokens stored.", bootstrap.Tenant, a.Profile())
	}

	return nil
}

// storedAPIKeyDecider returns the check that decides, once the tenant is
// picked, whether login keeps the API key of the profile. It reads the
// keychain alone: pmg and vet read the key from there, so an API key in the
// environment must not stop login from replacing an expired keychain key.
func storedAPIKeyDecider(a *app.App) func(context.Context, string) cliauth.APIKeyDecision {
	return func(ctx context.Context, tenant string) cliauth.APIKeyDecision {
		return cliauth.DecideStoredAPIKey(ctx, storedAPIKey(a), tenant, time.Now(), cliauth.VerifyAPIKey)
	}
}

// storedAPIKey returns the API key credentials of the profile in the
// keychain, or nil when there are none. A keychain that cannot be read counts
// as no key, so login creates one.
func storedAPIKey(a *app.App) *cloud.Credentials {
	resolver, err := cloud.NewKeychainCredentialResolver(cloud.CredentialTypeAPIKey, a.KeychainOptions()...)
	if err != nil {
		log.Warnf("auth login: stored api key: %v", err)
		return nil
	}
	defer func() {
		if err := resolver.Close(); err != nil {
			log.Warnf("auth login: close keychain resolver: %v", err)
		}
	}()

	creds, err := resolver.Resolve()
	if err != nil {
		if !errors.Is(err, cloud.ErrMissingCredentials) {
			log.Warnf("auth login: stored api key: %v", err)
		}
		return nil
	}
	return creds
}

func resolveAPIKey(flags loginFlags) (string, error) {
	if v := strings.TrimSpace(flags.apiKeyValue); v != "" {
		return v, nil
	}
	if flags.apiKeyFromStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read api key from stdin: %w", err)
		}
		v := strings.TrimSpace(string(data))
		if v == "" {
			return "", errors.New("--from-stdin: empty input")
		}
		return v, nil
	}
	if v := apiKeyFromEnvResolver(); v != "" {
		return v, nil
	}

	var apiKey string
	if err := huh.NewInput().
		Title("API key").
		Description("Create one at app.safedep.io → Settings → API keys").
		EchoMode(huh.EchoModePassword).
		Value(&apiKey).
		Run(); err != nil {
		return "", fmt.Errorf("api key prompt: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", errors.New("api key is required")
	}
	return apiKey, nil
}

func resolveTenant(_ *cobra.Command, a *app.App, flag string) (string, error) {
	if v := strings.TrimSpace(flag); v != "" {
		return v, nil
	}

	if t := tenantFromResolver(a.APIKeyResolver); t != "" {
		return t, nil
	}
	if t := tenantFromResolver(a.TokenResolver); t != "" {
		return t, nil
	}

	var tenant string
	if err := huh.NewInput().
		Title("Tenant domain").
		Description("Example: acme-corp.safedep.io").
		Value(&tenant).
		Run(); err != nil {
		return "", fmt.Errorf("tenant prompt: %w", err)
	}
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return "", errors.New("tenant is required")
	}
	return tenant, nil
}

// apiKeyFromEnvResolver returns the SAFEDEP_API_KEY value via dry/cloud's
// env credential resolver. SAFEDEP_TENANT_ID is not required here: tenant
// resolution happens separately (flag, then keychain, then prompt) and
// the env resolver constructs valid data-plane credentials with just the
// API key. Routing through dry/cloud rather than os.Getenv keeps the
// env-var contract owned upstream so the CLI matches vet and pmg.
func apiKeyFromEnvResolver() string {
	r, err := cloud.NewEnvCredentialResolver()
	if err != nil {
		log.Warnf("auth login: env resolver: %v", err)
		return ""
	}
	creds, err := r.Resolve()
	if err != nil {
		return ""
	}
	k, err := creds.GetAPIKey()
	if err != nil {
		return ""
	}
	return k
}

// tenantFromResolver invokes the supplied resolver factory and returns the
// stored tenant domain. Missing-credentials errors are non-fatal here:
// they mean the profile has no entry of that type yet, and the caller will
// fall through to the next source.
func tenantFromResolver(get func() (cloud.CredentialResolver, error)) string {
	resolver, err := get()
	if err != nil {
		log.Warnf("auth login: tenant lookup: %v", err)
		return ""
	}
	creds, err := resolver.Resolve()
	if err != nil {
		if !errors.Is(err, cloud.ErrMissingCredentials) {
			log.Warnf("auth login: tenant lookup: resolve: %v", err)
		}
		return ""
	}
	t, err := creds.GetTenantDomain()
	if err != nil {
		log.Warnf("auth login: tenant lookup: domain: %v", err)
		return ""
	}
	return t
}
