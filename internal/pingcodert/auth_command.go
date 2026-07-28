package pingcodert

import (
	"fmt"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/restishengine"
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
	authArgs, handled, err := authflow.LocalCommandArgs(args, "auth")
	if err != nil || !handled {
		return handled, err
	}
	configureStatePaths()
	return true, RunAuth(authArgs, defaultAuthIO())
}

// RunAuth executes login/status/logout subcommands.
func RunAuth(args []string, authIO AuthIO) error {
	authIO = authIO.Normalize()
	if len(args) == 0 || authflow.IsHelpArg(args[0]) {
		fmt.Fprint(authIO.Stdout, authHelpText())
		return nil
	}
	switch args[0] {
	case "login":
		return runAuthLogin(args[1:], authIO)
	case "status":
		return runAuthStatus(args[1:], authIO)
	case "logout":
		return runAuthLogout(args[1:], authIO)
	default:
		return fmt.Errorf("unknown auth command %q\n\n%s", args[0], authHelpText())
	}
}

func authHelpText() string {
	return `Usage:
  pingcode auth login --mode client|token
  pingcode auth status
  pingcode auth logout

Manage Base URL and credentials stored in config.toml.
Secrets are entered interactively and are never accepted on argv.
`
}

func loginHelpText() string {
	return `Usage:
  pingcode auth login --mode client
  pingcode auth login --mode token

Prompts:
  Base URL          service origin (HTTPS; HTTP only for localhost)
  Client ID/Secret  for --mode client
  Access Token      for --mode token
`
}

func runAuthLogin(args []string, authIO AuthIO) error {
	mode := ""
	for i := 0; i < len(args); i++ {
		switch {
		case authflow.IsHelpArg(args[i]):
			fmt.Fprint(authIO.Stdout, loginHelpText())
			return nil
		case args[i] == "--mode":
			if i+1 >= len(args) {
				return fmt.Errorf("--mode requires a value")
			}
			mode = args[i+1]
			i++
		case args[i] == "--client-secret" || args[i] == "--access-token" || args[i] == "--client-id" || args[i] == "--base-url":
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

func runAuthStatus(args []string, authIO AuthIO) error {
	if len(args) != 0 {
		return fmt.Errorf("auth status does not accept arguments")
	}
	path := ConfigPath()
	report := authflow.StatusReport{ConfigPath: path}
	session, _, _, err := loadSessionSnapshot()
	if err != nil {
		return err
	}
	if session.BaseURL != "" {
		report.BaseURL = session.BaseURL
		report.BaseURLSource = session.BaseURLSource
	}
	if !session.HasCredentials {
		return authflow.WriteJSON(authIO.Stdout, report)
	}
	report.Configured = true
	report.Mode = session.Credentials.Mode
	report.CredentialSource = session.CredentialSource
	return authflow.WriteJSON(authIO.Stdout, report)
}

func runAuthLogout(args []string, authIO AuthIO) error {
	if len(args) != 0 {
		return fmt.Errorf("auth logout does not accept arguments")
	}
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
	path := restishengine.TokenCachePath(ConfigDir())
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat PingCode token cache: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("PingCode token cache must be a regular file and not a symlink: %s", path)
	}
	if err := restishauth.NewTokenCache(path).DeletePrefix("pingcode:"); err != nil {
		return fmt.Errorf("clear PingCode token cache: %w", err)
	}
	return nil
}
