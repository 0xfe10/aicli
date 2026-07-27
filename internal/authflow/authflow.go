// Package authflow provides shared interactive auth helpers for service CLIs.
package authflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	SourceEnvironment = "environment"
	SourceConfig      = "config"
	SourceDefault     = "default"
)

// IO controls interactive prompts. Tests inject fakes.
type IO struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	ReadSecret func(prompt string) (string, error)
}

// DefaultIO returns terminal-backed interactive IO.
func DefaultIO() IO {
	return IO{
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

// Normalize fills missing writers/readers with process defaults.
func (io IO) Normalize() IO {
	if io.Stdin == nil {
		io.Stdin = os.Stdin
	}
	if io.Stdout == nil {
		io.Stdout = os.Stdout
	}
	if io.Stderr == nil {
		io.Stderr = os.Stderr
	}
	return io
}

// PromptLine reads a trimmed non-secret line from Stdin.
// It reads one byte at a time so multiple prompts do not lose buffered input.
func PromptLine(authIO IO, prompt string) (string, error) {
	authIO = authIO.Normalize()
	fmt.Fprint(authIO.Stderr, prompt)
	var buf strings.Builder
	tmp := make([]byte, 1)
	for {
		n, err := authIO.Stdin.Read(tmp)
		if n > 0 {
			if tmp[0] == '\n' {
				break
			}
			if tmp[0] != '\r' {
				buf.WriteByte(tmp[0])
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}
	return strings.TrimSpace(buf.String()), nil
}

// PromptSecret reads a hidden secret using ReadSecret.
func PromptSecret(authIO IO, prompt string) (string, error) {
	authIO = authIO.Normalize()
	if authIO.ReadSecret == nil {
		return "", fmt.Errorf("secret prompt is unavailable")
	}
	return authIO.ReadSecret(prompt)
}

// PromptBaseURL asks for and validates a service Base URL.
func PromptBaseURL(authIO IO) (string, error) {
	raw, err := PromptLine(authIO, "Base URL: ")
	if err != nil {
		return "", err
	}
	return NormalizeBaseURL(raw)
}

// NormalizeBaseURL validates and canonicalizes a service Base URL.
// Production hosts require HTTPS; only localhost/127.0.0.1/::1 may use HTTP.
// Userinfo, query, and fragment are rejected. Trailing slashes are removed.
// Paths are preserved so services may be mounted under a subpath.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("Base URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("Base URL must be a valid absolute URL")
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("Base URL must be an absolute HTTP URL")
	}
	if u.User != nil {
		return "", fmt.Errorf("Base URL must not include username or password")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("Base URL must not include query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", fmt.Errorf("Base URL must use HTTPS (HTTP is allowed for localhost)")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// StatusReport is the shared JSON shape for `<cli> auth status`.
type StatusReport struct {
	Configured       bool   `json:"configured"`
	Mode             string `json:"mode,omitempty"`
	BaseURL          string `json:"baseUrl,omitempty"`
	BaseURLSource    string `json:"baseUrlSource,omitempty"`
	CredentialSource string `json:"credentialSource,omitempty"`
	ConfigPath       string `json:"configPath,omitempty"`
}

// WriteJSON writes an indented JSON document.
func WriteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
