package auth

import (
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

	res, err := cliauth.RunDeviceFlow(cmd.Context(), cliauth.PrintVerification)
	if err != nil {
		if !errors.Is(err, cliauth.ErrEmailNotVerified) {
			return err
		}
		tui.Warning("%v", err)
		tui.Info("Press Enter once you have verified your email to try again…")
		if _, scanErr := fmt.Fscanln(cmd.InOrStdin()); scanErr != nil && !errors.Is(scanErr, io.EOF) {
			log.Warnf("auth login: read stdin: %v", scanErr)
		}
		res, err = cliauth.RunDeviceFlow(cmd.Context(), cliauth.PrintVerification)
		if err != nil {
			if errors.Is(err, cliauth.ErrEmailNotVerified) {
				return errors.New("email still not verified: verify your email and run 'safedep auth login' again")
			}
			return err
		}
	}

	preferredTenant := flags.tenant
	if preferredTenant == "" {
		preferredTenant = tenantFromResolver(a.TokenResolver)
		if preferredTenant == "" {
			preferredTenant = tenantFromResolver(a.APIKeyResolver)
		}
	}

	createKey, name := apiKeyCreationPlan(a, preferredTenant, flags)

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

	switch {
	case bootstrap.APIKey != "":
		tui.Success("Authenticated as %s (profile %q). API key created, expires %s.",
			bootstrap.Tenant, a.Profile(), bootstrap.APIKeyExpiresAt.UTC().Format("2006-01-02"))
	case createKey:
		tui.Success("Authenticated as %s (profile %q). API key creation skipped — none in response.", bootstrap.Tenant, a.Profile())
	default:
		tui.Success("Authenticated as %s (profile %q). OAuth tokens stored.", bootstrap.Tenant, a.Profile())
	}

	return nil
}

// apiKeyCreationPlan decides whether to ask the bootstrap to create an API
// key, and what to name it. --no-api-key wins. --rotate-api-key always
// creates. Otherwise we skip when the profile already holds a key for the
// preferred tenant.
func apiKeyCreationPlan(a *app.App, preferredTenant string, flags loginFlags) (bool, string) {
	if flags.noAPIKey {
		return false, ""
	}
	if !flags.rotateAPIKey && hasAPIKeyForTenant(a, preferredTenant) {
		log.Warnf("auth login: existing API key kept; use --rotate-api-key to create a new one")
		return false, ""
	}
	return true, cliauth.APIKeyName(cliauth.Hostname(), time.Now())
}

func hasAPIKeyForTenant(a *app.App, tenant string) bool {
	if tenant == "" {
		return false
	}
	resolver, err := a.APIKeyResolver()
	if err != nil {
		log.Warnf("auth login: api-key check: %v", err)
		return false
	}
	creds, err := resolver.Resolve()
	if err != nil {
		return false
	}
	got, err := creds.GetTenantDomain()
	if err != nil {
		return false
	}
	return got == tenant
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
