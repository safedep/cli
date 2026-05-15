package mcp

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/safedep/cli/internal/agent"
	cliauth "github.com/safedep/cli/internal/auth"
	"github.com/safedep/cli/internal/endpoint"
	"github.com/safedep/dry/cloud"
	"github.com/safedep/dry/cloud/endpointsync"
	"github.com/safedep/dry/log"
	"github.com/safedep/dry/tui"
)

type installInput struct {
	APIKeyResolver  func() (cloud.CredentialResolver, error)
	CredentialStore cloud.CredentialStore
	MCPURL          string
	WorkspaceDir    string
	Force           bool
}

type setupMCPService struct {
	agents   []agent.Agent
	resolver endpointsync.EndpointIdentityResolver
}

func (s *setupMCPService) install(ctx context.Context, in installInput) error {
	apiKey, tenantID, credentialsSaved, err := s.resolveCredentials(ctx, in)
	if err != nil {
		return err
	}

	cfg, err := endpoint.BuildMCPConfig(in.MCPURL, apiKey, tenantID, s.resolver)
	if err != nil {
		s.warnMCPFailed(credentialsSaved, "%v", err)
		return nil
	}

	detected := agent.FilterDetected(s.agents)
	if len(detected) == 0 {
		tui.Warning("No supported AI agents detected on this machine.")
		return nil
	}

	if err := agent.InjectAll(detected, cfg, in.WorkspaceDir); err != nil {
		s.warnMCPFailed(credentialsSaved, "%v", err)
		return nil
	}

	tui.Success("SafeDep MCP server configured. You're ready to use AI agents with SafeDep protection.")
	return nil
}

// tryExistingCredentials attempts to resolve API key and tenant ID from the
// existing credential store. Returns ok=false on any failure, logging
// unexpected errors as warnings.
func (s *setupMCPService) tryExistingCredentials(in installInput) (apiKey, tenantID string, ok bool) {
	resolver, err := in.APIKeyResolver()
	if err != nil {
		log.Warnf("setup mcp: credential resolver: %v", err)
		return "", "", false
	}

	creds, err := resolver.Resolve()
	if err != nil {
		if !errors.Is(err, cloud.ErrMissingCredentials) {
			log.Warnf("setup mcp: resolve credentials: %v", err)
		}
		return "", "", false
	}

	key, err := creds.GetAPIKey()
	if err != nil {
		return "", "", false
	}

	tenant, err := creds.GetTenantDomain()
	if err != nil || tenant == "" {
		return "", "", false
	}

	return key, tenant, true
}

// resolveCredentials returns the API key and tenant ID to use for MCP
// configuration. It returns credentialsSaved=true when the device flow ran
// and credentials were written to the keychain.
func (s *setupMCPService) resolveCredentials(ctx context.Context, in installInput) (apiKey, tenantID string, credentialsSaved bool, err error) {
	if !in.Force {
		if key, tenant, ok := s.tryExistingCredentials(in); ok {
			return key, tenant, false, nil
		}
	}

	// Slow path: full device flow for first-timers or when --force is set.
	tui.Info("Starting SafeDep onboarding...")

	res, err := cliauth.RunDeviceFlow(ctx, cliauth.PrintVerification,
		cliauth.EmailVerificationRetry(os.Stdin))
	if err != nil {
		return "", "", false, err
	}

	bootstrap, err := cliauth.PostOAuthBootstrap(ctx, cliauth.BootstrapInput{
		AccessToken:          res.AccessToken,
		CreateAPIKey:         true,
		APIKeyName:           cliauth.APIKeyName(cliauth.Hostname(), time.Now()),
		APIKeyExpiryDays:     cliauth.DefaultAPIKeyExpiryDays,
		Picker:               cliauth.PromptTenantPicker,
		RegistrationPrompter: cliauth.NewRegistrationPrompter(res.AccessToken),
	})
	if err != nil {
		return "", "", false, err
	}

	if err := cliauth.SaveBootstrapResult(in.CredentialStore, res.AccessToken, res.RefreshToken, bootstrap); err != nil {
		return "", "", false, err
	}

	return bootstrap.APIKey, bootstrap.Tenant, true, nil
}

func (s *setupMCPService) warnMCPFailed(credentialsSaved bool, format string, args ...any) {
	if credentialsSaved {
		tui.Warning("Credentials saved. MCP configuration failed: "+format, args...)
	} else {
		tui.Warning("MCP configuration failed: "+format, args...)
	}
	tui.Info("Fix the issue and run 'safedep protect mcp install' to retry.")
}
