package pingcodert

import (
	"fmt"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

// AuthIO controls interactive prompts for auth commands. Tests inject fakes.
type AuthIO = authflow.IO

func defaultAuthIO() AuthIO {
	return authflow.DefaultIO()
}

// MaybeRunAuth handles `pingcode auth ...` before Restish sees argv.
// handled is true when args select the auth control plane.
func MaybeRunAuth(args []string) (handled bool, err error) {
	if len(args) < 2 || args[1] != "auth" {
		return false, nil
	}
	configureStatePaths()
	return true, RunAuth(args[2:], defaultAuthIO())
}

// RunAuth executes login/status/logout subcommands.
func RunAuth(args []string, authIO AuthIO) error {
	authIO = authIO.Normalize()
	if len(args) == 0 {
		return fmt.Errorf("usage: pingcode auth login|status|logout")
	}
	switch args[0] {
	case "login":
		return runAuthLogin(args[1:], authIO)
	case "status":
		return runAuthStatus(authIO)
	case "logout":
		return runAuthLogout(authIO)
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func runAuthLogin(args []string, authIO AuthIO) error {
	mode := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mode":
			if i+1 >= len(args) {
				return fmt.Errorf("--mode requires a value")
			}
			mode = args[i+1]
			i++
		case "--client-secret", "--access-token", "--client-id", "--base-url":
			return fmt.Errorf("%s is not supported; enter values interactively to avoid shell history exposure", args[i])
		default:
			return fmt.Errorf("unknown login flag %q", args[i])
		}
	}
	mode = strings.TrimSpace(mode)
	switch mode {
	case AuthModeClient:
		return loginClient(authIO)
	case AuthModeToken:
		return loginToken(authIO)
	case "":
		return fmt.Errorf("usage: pingcode auth login --mode client|token")
	default:
		return fmt.Errorf("unsupported login mode %q: expected client or token", mode)
	}
}

func loginClient(authIO AuthIO) error {
	baseURL, err := authflow.PromptBaseURL(authIO)
	if err != nil {
		return err
	}
	clientID, err := authflow.PromptLine(authIO, "Client ID: ")
	if err != nil {
		return err
	}
	if clientID == "" {
		return fmt.Errorf("Client ID is required")
	}
	secret, err := authflow.PromptSecret(authIO, "Client Secret: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("Client Secret is required")
	}
	if err := SaveLogin(ConfigPath(), baseURL, &AuthConfig{
		Mode:         AuthModeClient,
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(secret),
	}); err != nil {
		return err
	}
	if err := clearCachedClientCredentialsTokens(); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials saved.")
	return nil
}

func loginToken(authIO AuthIO) error {
	baseURL, err := authflow.PromptBaseURL(authIO)
	if err != nil {
		return err
	}
	token, err := authflow.PromptSecret(authIO, "Access Token: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("Access Token is required")
	}
	if err := SaveLogin(ConfigPath(), baseURL, &AuthConfig{
		Mode:        AuthModeToken,
		AccessToken: strings.TrimSpace(token),
	}); err != nil {
		return err
	}
	if err := clearCachedClientCredentialsTokens(); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials saved.")
	return nil
}

func runAuthStatus(authIO AuthIO) error {
	path := ConfigPath()
	report := authflow.StatusReport{ConfigPath: path}

	baseURL, baseSource, err := ResolveBaseURL(path)
	if err != nil {
		return err
	}
	if baseURL != "" {
		report.BaseURL = baseURL
		report.BaseURLSource = baseSource
	}

	creds, err := resolveCredentials(path)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			return authflow.WriteJSON(authIO.Stdout, report)
		}
		return err
	}
	report.Configured = true
	report.Mode = creds.Mode
	report.CredentialSource = creds.Source
	return authflow.WriteJSON(authIO.Stdout, report)
}

func runAuthLogout(authIO AuthIO) error {
	path := ConfigPath()
	if err := ClearAuthConfig(path); err != nil {
		return err
	}
	if err := clearCachedClientCredentialsTokens(); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials removed.")
	if EnvironmentAuthPresent() {
		fmt.Fprintln(authIO.Stderr, "warning: authorization environment variables are still set; this process will continue to use environment authorization")
	}
	return nil
}

func clearCachedClientCredentialsTokens() error {
	path := restishauth.DefaultTokenCachePath()
	if path == "" {
		return nil
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat PingCode token cache: %w", err)
	}
	if err := restishauth.NewTokenCache(path).DeletePrefix(""); err != nil {
		return fmt.Errorf("clear PingCode token cache: %w", err)
	}
	return nil
}
