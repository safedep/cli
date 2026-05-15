package auth

import (
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/cli/browser"
	"github.com/safedep/dry/log"
	"github.com/safedep/dry/tui"
	tuiout "github.com/safedep/dry/tui/output"
)

// NewRegistrationPrompter returns a BootstrapInput-compatible RegistrationPrompter
// closure that calls PromptRegistration with the given access token.
func NewRegistrationPrompter(accessToken string) func() (*RegistrationInput, error) {
	return func() (*RegistrationInput, error) {
		reg, err := PromptRegistration(accessToken)
		if err != nil {
			return nil, err
		}
		return &reg, nil
	}
}

// PromptRegistration collects first-time registration data via interactive huh
// forms. The email from the access token is shown and editable; the final
// value is included in the returned RegistrationInput.
func PromptRegistration(accessToken string) (RegistrationInput, error) {
	email := EmailFromAccessToken(accessToken)
	if email == "" {
		email = "unknown"
	}

	var name, orgName, orgDomain string

	nameForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Email").
				Description("This is the email associated with your account (shown for reference only).").
				Value(&email).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return errors.New("email is required")
					}
					if _, err := mail.ParseAddress(s); err != nil {
						return errors.New("enter a valid email address")
					}
					return nil
				}),
			huh.NewInput().
				Title("Your name").
				Description("Full name for your account.").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Organization name").
				Description("Name of your organization or project.").
				Value(&orgName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("organization name is required")
					}
					return nil
				}),
		),
	)
	if err := nameForm.Run(); err != nil {
		return RegistrationInput{}, fmt.Errorf("registration prompt: %w", err)
	}

	email = strings.TrimSpace(email)
	name = strings.TrimSpace(name)
	orgName = strings.TrimSpace(orgName)

	// Default the domain from the org name collected above.
	orgDomain = GenerateTenantDomain(orgName)

	domainForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Organization domain").
				Description("Unique slug for your tenant (e.g. acme-corp-brisk-harbor-xyz).").
				Value(&orgDomain).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("organization domain is required")
					}
					return nil
				}),
		),
	)
	if err := domainForm.Run(); err != nil {
		return RegistrationInput{}, fmt.Errorf("registration prompt: %w", err)
	}

	orgDomain = strings.TrimSpace(orgDomain)

	return RegistrationInput{
		Email:              email,
		Name:               name,
		OrganizationName:   orgName,
		OrganizationDomain: orgDomain,
	}, nil
}

// PromptTenantPicker presents a selection form when the user has access to
// multiple tenants and must choose one for the active profile.
func PromptTenantPicker(tenants []string) (string, error) {
	options := make([]huh.Option[string], 0, len(tenants))
	for _, t := range tenants {
		options = append(options, huh.NewOption(t, t))
	}

	var pick string
	if err := huh.NewSelect[string]().
		Title("Select tenant").
		Description("You have access to multiple tenants. Pick one for this profile.").
		Options(options...).
		Value(&pick).
		Run(); err != nil {
		return "", fmt.Errorf("tenant picker: %w", err)
	}
	if pick == "" {
		return "", errors.New("no tenant selected")
	}
	return pick, nil
}

// PrintVerification prints device-flow verification details and, in rich mode,
// attempts to open the browser automatically.
func PrintVerification(verificationURL, userCode string) {
	tui.Info("Open the following URL to complete authentication:")
	tui.Info("  %s", verificationURL)
	tui.Info("Verification code: %s", userCode)

	switch tuiout.CurrentMode() {
	case tuiout.Rich:
		if err := browser.OpenURL(verificationURL); err != nil {
			log.Warnf("auth: open browser: %v", err)
		}
	default:
	}
}

// EmailVerificationRetry returns a DeviceFlowRetry that handles the
// email-not-verified case: warns the user, waits for them to press Enter
// after verifying, then allows one retry. stdin is typically os.Stdin;
// callers may inject a reader for testing.
func EmailVerificationRetry(stdin io.Reader) DeviceFlowRetry {
	return DeviceFlowRetry{
		ShouldRetry: func(err error) bool { return errors.Is(err, ErrEmailNotVerified) },
		OnRetry: func(err error) {
			tui.Warning("%v", err)
			tui.Info("Press Enter once you have verified your email to try again…")
			if _, scanErr := fmt.Fscanln(stdin); scanErr != nil && !errors.Is(scanErr, io.EOF) {
				log.Warnf("auth: read stdin for email retry: %v", scanErr)
			}
		},
		OnExhausted: func() error {
			return errors.New("email still not verified: verify your email and run the command again")
		},
	}
}

// Hostname returns the machine hostname, falling back to "unknown" on error.
func Hostname() string {
	h, err := os.Hostname()
	if err != nil {
		log.Warnf("auth: hostname: %v", err)
		return "unknown"
	}
	return h
}
