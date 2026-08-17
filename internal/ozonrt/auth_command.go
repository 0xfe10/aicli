package ozonrt

import (
	"fmt"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/contextflow"
)

type AuthIO = authflow.IO

func MaybeRunAuth(args []string) (bool, error) {
	return MaybeRunAuthWithContext(args, contextManager.DefaultSelection())
}

// MaybeRunAuthWithContext handles auth for one selected context.
func MaybeRunAuthWithContext(args []string, selection contextflow.Selection) (bool, error) {
	authArgs, handled, err := authflow.LocalCommandArgs(args, "auth")
	if err != nil || !handled {
		return handled, err
	}
	configureStatePathsFor(selection)
	return true, RunAuthWithContext(authArgs, authflow.DefaultIO(), selection)
}

func RunAuth(args []string, authIO AuthIO) error {
	return RunAuthWithContext(args, authIO, contextManager.DefaultSelection())
}

// RunAuthWithContext executes auth commands against selection.
func RunAuthWithContext(args []string, authIO AuthIO, selection contextflow.Selection) error {
	authIO = authIO.Normalize()
	if len(args) == 0 || authflow.IsHelpArg(args[0]) {
		fmt.Fprint(authIO.Stdout, authHelp())
		return nil
	}
	switch args[0] {
	case "login":
		return authLogin(args[1:], authIO, selection)
	case "status":
		return authStatus(args[1:], authIO, selection)
	case "logout":
		return authLogout(args[1:], authIO, selection)
	default:
		return fmt.Errorf("unknown auth command %q\n\n%s", args[0], authHelp())
	}
}

func authHelp() string {
	return `Usage:
  ozon auth login --mode key
  ozon auth status
  ozon auth logout

Manage Base URL, Client-Id, and Api-Key stored in config.toml.
The Api-Key is entered interactively and is never accepted on argv.
`
}

func authLogin(args []string, authIO AuthIO, selection contextflow.Selection) error {
	mode := ""
	for i := 0; i < len(args); i++ {
		switch {
		case authflow.IsHelpArg(args[i]):
			fmt.Fprint(authIO.Stdout, "Usage: ozon auth login --mode key\n")
			return nil
		case args[i] == "--mode":
			if i+1 >= len(args) {
				return fmt.Errorf("--mode requires a value")
			}
			mode = args[i+1]
			i++
		case args[i] == "--api-key" || args[i] == "--client-id" || args[i] == "--base-url":
			return fmt.Errorf("%s is not supported; enter values interactively to avoid shell history exposure", args[i])
		default:
			return fmt.Errorf("unknown login flag %q", args[i])
		}
	}
	if strings.TrimSpace(mode) != AuthModeKey {
		return fmt.Errorf("usage: ozon auth login --mode key")
	}
	baseURL, err := authflow.PromptBaseURL(authIO)
	if err != nil {
		return err
	}
	clientID, err := authflow.PromptLine(authIO, "Client-Id: ")
	if err != nil {
		return err
	}
	apiKey, err := authflow.PromptSecret(authIO, "Api-Key: ")
	if err != nil {
		return err
	}
	if err := SaveLogin(ConfigPathFor(selection), baseURL, &AuthConfig{Mode: AuthModeKey, ClientID: clientID, APIKey: apiKey}); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials saved.")
	return nil
}

func authStatus(args []string, authIO AuthIO, selection contextflow.Selection) error {
	if len(args) != 0 {
		return fmt.Errorf("auth status does not accept arguments")
	}
	session, _, err := LoadSessionWithContext(selection)
	if err != nil {
		return err
	}
	report := authflow.StatusReport{
		Configured:       session.HasCredentials,
		Context:          selection.Name,
		ContextSource:    selection.Source,
		BaseURL:          session.BaseURL,
		BaseURLSource:    session.BaseURLSource,
		CredentialSource: session.CredentialSource,
		ConfigPath:       ConfigPathFor(selection),
	}
	if session.HasCredentials {
		report.Mode = AuthModeKey
	}
	return authflow.WriteJSON(authIO.Stdout, report)
}

func authLogout(args []string, authIO AuthIO, selection contextflow.Selection) error {
	if len(args) != 0 {
		return fmt.Errorf("auth logout does not accept arguments")
	}
	if err := ClearAuthConfig(ConfigPathFor(selection)); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials removed.")
	if EnvironmentAuthPresent() {
		fmt.Fprintln(authIO.Stderr, "warning: OZON_CLIENT_ID or OZON_API_KEY is still set; environment authorization remains active")
	}
	return nil
}
