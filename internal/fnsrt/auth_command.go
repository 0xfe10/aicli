package fnsrt

import (
	"fmt"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/contextflow"
)

// AuthIO controls interactive prompts for auth commands.
type AuthIO = authflow.IO

func defaultAuthIO() AuthIO {
	return authflow.DefaultIO()
}

// MaybeRunAuth handles `fns auth ...` before Restish sees argv.
func MaybeRunAuth(args []string) (handled bool, err error) {
	return MaybeRunAuthWithContext(args, contextManager.DefaultSelection())
}

// MaybeRunAuthWithContext handles auth for one selected context.
func MaybeRunAuthWithContext(args []string, selection contextflow.Selection) (handled bool, err error) {
	authArgs, handled, err := authflow.LocalCommandArgs(args, "auth")
	if err != nil || !handled {
		return handled, err
	}
	configureStatePathsFor(selection)
	return true, RunAuthWithContext(authArgs, defaultAuthIO(), selection)
}

// RunAuth executes login/status/logout subcommands.
func RunAuth(args []string, authIO AuthIO) error {
	return RunAuthWithContext(args, authIO, contextManager.DefaultSelection())
}

// RunAuthWithContext executes auth commands against selection.
func RunAuthWithContext(args []string, authIO AuthIO, selection contextflow.Selection) error {
	authIO = authIO.Normalize()
	if len(args) == 0 || authflow.IsHelpArg(args[0]) {
		fmt.Fprint(authIO.Stdout, authHelpText())
		return nil
	}
	switch args[0] {
	case "login":
		return runAuthLogin(args[1:], authIO, selection)
	case "status":
		return runAuthStatus(args[1:], authIO, selection)
	case "logout":
		return runAuthLogout(args[1:], authIO, selection)
	default:
		return fmt.Errorf("unknown auth command %q\n\n%s", args[0], authHelpText())
	}
}

func authHelpText() string {
	return `Usage:
  fns auth login --mode token
  fns auth status
  fns auth logout

Manage Base URL and credentials stored in config.toml.
Secrets are entered interactively and are never accepted on argv.
`
}

func loginHelpText() string {
	return `Usage:
  fns auth login --mode token

Prompts:
  Base URL       tenant origin (HTTPS; HTTP only for localhost)
  Access Token   Bearer token from the FNS WebGUI
`
}

func runAuthLogin(args []string, authIO AuthIO, selection contextflow.Selection) error {
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
		case args[i] == "--access-token" || args[i] == "--base-url":
			return fmt.Errorf("%s is not supported; enter values interactively to avoid shell history exposure", args[i])
		default:
			return fmt.Errorf("unknown login flag %q", args[i])
		}
	}
	mode = strings.TrimSpace(mode)
	switch mode {
	case AuthModeToken:
		return loginToken(authIO, selection)
	case "":
		return fmt.Errorf("usage: fns auth login --mode token")
	default:
		return fmt.Errorf("unsupported login mode %q: expected token", mode)
	}
}

func loginToken(authIO AuthIO, selection contextflow.Selection) error {
	baseURL, err := authflow.PromptBaseURL(authIO)
	if err != nil {
		return err
	}
	if IsPlaceholderBaseURL(baseURL) {
		return fmt.Errorf("Base URL must not use the placeholder host %q", PlaceholderHost)
	}
	token, err := authflow.PromptSecret(authIO, "Access Token: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("Access Token is required")
	}
	if err := SaveLogin(ConfigPathFor(selection), baseURL, &AuthConfig{
		Mode:        AuthModeToken,
		AccessToken: strings.TrimSpace(token),
	}); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials saved.")
	return nil
}

func runAuthStatus(args []string, authIO AuthIO, selection contextflow.Selection) error {
	if len(args) != 0 {
		return fmt.Errorf("auth status does not accept arguments")
	}
	path := ConfigPathFor(selection)
	report := authflow.StatusReport{Context: selection.Name, ContextSource: selection.Source, ConfigPath: path}
	session, _, _, err := loadSessionSnapshotWithContext(selection)
	if err != nil {
		return err
	}
	usableBase := HasUsableBaseURL(session.BaseURL, session.BaseURLSource)
	if usableBase {
		report.BaseURL = session.BaseURL
		report.BaseURLSource = session.BaseURLSource
	}
	if !session.HasCredentials {
		return authflow.WriteJSON(authIO.Stdout, report)
	}
	report.Mode = AuthModeToken
	report.CredentialSource = session.CredentialSource
	report.Configured = usableBase
	return authflow.WriteJSON(authIO.Stdout, report)
}

func runAuthLogout(args []string, authIO AuthIO, selection contextflow.Selection) error {
	if len(args) != 0 {
		return fmt.Errorf("auth logout does not accept arguments")
	}
	if err := ClearAuthConfig(ConfigPathFor(selection)); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials removed.")
	if EnvironmentAuthPresent() {
		fmt.Fprintln(authIO.Stderr, "warning: FNS_ACCESS_TOKEN is still set; this process will continue to use environment authorization")
	}
	return nil
}
