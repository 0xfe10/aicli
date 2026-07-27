package fnsrt

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// AuthIO controls interactive prompts for auth commands.
type AuthIO struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	ReadSecret func(prompt string) (string, error)
}

func defaultAuthIO() AuthIO {
	return AuthIO{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		ReadSecret: func(prompt string) (string, error) {
			fmt.Fprint(os.Stderr, prompt)
			secret, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return "", err
			}
			return string(secret), nil
		},
	}
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
	if authIO.Stdout == nil {
		authIO.Stdout = os.Stdout
	}
	if authIO.Stderr == nil {
		authIO.Stderr = os.Stderr
	}
	if authIO.Stdin == nil {
		authIO.Stdin = os.Stdin
	}
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
		case "--access-token":
			return fmt.Errorf("%s is not supported; enter secrets interactively to avoid shell history exposure", args[i])
		default:
			return fmt.Errorf("unknown login flag %q", args[i])
		}
	}
	mode = strings.TrimSpace(mode)
	switch mode {
	case "token":
		return loginToken(authIO)
	case "":
		return fmt.Errorf("usage: fns auth login --mode token")
	default:
		return fmt.Errorf("unsupported login mode %q: expected token", mode)
	}
}

func loginToken(authIO AuthIO) error {
	token, err := readSecret(authIO, "Access Token: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("Access Token is required")
	}
	if err := SaveAccessToken(ConfigPath(), token); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials saved.")
	return nil
}

type authStatusReport struct {
	Configured bool   `json:"configured"`
	HasToken   bool   `json:"hasToken"`
	Source     string `json:"source,omitempty"`
	ConfigPath string `json:"configPath,omitempty"`
}

func runAuthStatus(authIO AuthIO) error {
	path := ConfigPath()
	report := authStatusReport{ConfigPath: path}
	creds, err := resolveCredentials(path)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			return writeJSON(authIO.Stdout, report)
		}
		return err
	}
	report.Configured = true
	report.HasToken = creds.AccessToken != ""
	report.Source = creds.Source
	return writeJSON(authIO.Stdout, report)
}

func runAuthLogout(authIO AuthIO) error {
	if err := ClearAccessToken(ConfigPath()); err != nil {
		return err
	}
	fmt.Fprintln(authIO.Stdout, "Credentials removed.")
	if EnvironmentAuthPresent() {
		fmt.Fprintln(authIO.Stderr, "warning: FNS_ACCESS_TOKEN is still set; this process will continue to use environment authorization")
	}
	return nil
}

func readSecret(authIO AuthIO, prompt string) (string, error) {
	if authIO.ReadSecret != nil {
		return authIO.ReadSecret(prompt)
	}
	return "", fmt.Errorf("secret prompt is unavailable")
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
