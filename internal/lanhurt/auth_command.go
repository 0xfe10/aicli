package lanhurt

import (
	"fmt"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/contextflow"
)

func MaybeRunAuthWithContext(args []string, selection contextflow.Selection) (bool, error) {
	authArgs, handled, err := authflow.LocalCommandArgs(args, "auth")
	if err != nil || !handled {
		return handled, err
	}
	return true, runAuth(authArgs, authflow.DefaultIO(), selection)
}

func runAuth(args []string, authIO authflow.IO, selection contextflow.Selection) error {
	authIO = authIO.Normalize()
	if len(args) == 0 || authflow.IsHelpArg(args[0]) {
		fmt.Fprint(authIO.Stdout, "Usage:\n  lanhu auth login --mode cookie\n  lanhu auth status\n  lanhu auth logout\n")
		return nil
	}
	switch args[0] {
	case "login":
		if len(args) == 2 && authflow.IsHelpArg(args[1]) {
			fmt.Fprintln(authIO.Stdout, "Usage: lanhu auth login --mode cookie")
			return nil
		}
		if len(args) != 3 || args[1] != "--mode" || args[2] != AuthModeCookie {
			return fmt.Errorf("usage: lanhu auth login --mode cookie")
		}
		cookie, err := authflow.PromptSecret(authIO, "Lanhu Cookie: ")
		if err != nil {
			return err
		}
		dds, err := authflow.PromptSecret(authIO, "DDS Cookie (empty uses Lanhu Cookie): ")
		if err != nil {
			return err
		}
		if err := saveLogin(ConfigPathFor(selection), cookie, dds); err != nil {
			return err
		}
		fmt.Fprintln(authIO.Stdout, "Credentials saved.")
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("auth status does not accept arguments")
		}
		session, err := LoadSessionWithContext(selection)
		if err != nil {
			return err
		}
		report := authflow.StatusReport{Configured: session.HasCredentials, Context: selection.Name, ContextSource: selection.Source, BaseURL: DefaultBaseURL, BaseURLSource: authflow.SourceDefault, CredentialSource: session.CredentialSource, ConfigPath: ConfigPathFor(selection)}
		if session.HasCredentials {
			report.Mode = AuthModeCookie
		}
		return authflow.WriteJSON(authIO.Stdout, report)
	case "logout":
		if len(args) != 1 {
			return fmt.Errorf("auth logout does not accept arguments")
		}
		if err := clearLogin(ConfigPathFor(selection)); err != nil {
			return err
		}
		fmt.Fprintln(authIO.Stdout, "Credentials removed.")
		if EnvironmentAuthPresent() {
			fmt.Fprintln(authIO.Stderr, "warning: LANHU_COOKIE or DDS_COOKIE is still set; environment authorization remains active")
		}
		return nil
	default:
		return fmt.Errorf("unknown auth command %q", strings.TrimSpace(args[0]))
	}
}
