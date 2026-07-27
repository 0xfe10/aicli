package fnsrt

import (
	"fmt"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
)

// AuthIO controls interactive prompts for auth commands.
type AuthIO = authflow.IO

func defaultAuthIO() AuthIO {
	return authflow.DefaultIO()
}

// MaybeRunAuth handles `fns auth ...` before Restish sees argv.
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
		return fmt.Errorf("usage: fns auth login|status|logout")
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
		case "--access-token", "--base-url":
			return fmt.Errorf("%s is not supported; enter values interactively to avoid shell history exposure", args[i])
		default:
			return fmt.Errorf("unknown login flag %q", args[i])
		}
	}
	mode = strings.TrimSpace(mode)
	switch mode {
	case AuthModeToken:
		return loginToken(authIO)
	case "":
		return fmt.Errorf("usage: fns auth login --mode token")
	default:
		return fmt.Errorf("unsupported login mode %q: expected token", mode)
	}
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
	// Omit compile-time placeholder so status does not look configured.
	if baseSource != authflow.SourceDefault {
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
	report.Mode = AuthModeToken
	report.CredentialSource = creds.Source
	return authflow.WriteJSON(authIO.Stdout, report)
}

func runAuthLogout(authIO AuthIO) error {
	if err := ClearAuthConfig(ConfigPath()); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials removed.")
	if EnvironmentAuthPresent() {
		fmt.Fprintln(authIO.Stderr, "warning: FNS_ACCESS_TOKEN is still set; this process will continue to use environment authorization")
	}
	return nil
}
