// Package authflow provides shared interactive auth helpers for service CLIs.
package authflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/term"
)

const (
	SourceEnvironment = "environment"
	SourceConfig      = "config"
	SourceDefault     = "default"
)

var (
	bearerPattern   = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*`)
	secretKVPattern = regexp.MustCompile(`(?i)\b(access[_-]?token|client[_-]?secret|token)\s*[=:]\s*([^\s,;]+)`)
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
// Encoded path separators (%2F) are rejected so normalization cannot change
// gateway routing semantics.
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
	escaped := u.EscapedPath()
	if strings.Contains(strings.ToLower(escaped), "%2f") {
		return "", fmt.Errorf("Base URL must not contain encoded path separators")
	}
	host := CanonicalHostname(u.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", fmt.Errorf("Base URL must use HTTPS (HTTP is allowed for localhost)")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if host != "" {
		// Preserve ports and the brackets required by IPv6 URL authorities.
		if u.Port() != "" {
			u.Host = net.JoinHostPort(host, u.Port())
		} else if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.RawPath != "" {
		u.RawPath = strings.TrimRight(u.RawPath, "/")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// CanonicalHostname lowercases a hostname and strips a trailing DNS root dot.
func CanonicalHostname(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.TrimSuffix(host, ".")
}

// HostnameOf returns the canonical hostname for a URL string.
func HostnameOf(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid URL")
	}
	return CanonicalHostname(u.Hostname()), nil
}

// RequestUnderBaseURL reports whether req is same-origin with baseURL and
// under its path prefix. Credentials must not be attached otherwise.
func RequestUnderBaseURL(reqURL *url.URL, baseURL string) error {
	if reqURL == nil {
		return fmt.Errorf("request URL is required")
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("Base URL is invalid")
	}
	reqHost := CanonicalHostname(reqURL.Hostname())
	baseHost := CanonicalHostname(base.Hostname())
	if !strings.EqualFold(reqURL.Scheme, base.Scheme) || reqHost == "" || reqHost != baseHost {
		return fmt.Errorf("request host %q does not match configured Base URL host %q", reqHost, baseHost)
	}
	// Default HTTPS/HTTP ports are equivalent to an omitted port.
	if normalizePort(reqURL) != normalizePort(base) {
		return fmt.Errorf("request port does not match configured Base URL")
	}
	basePath := strings.TrimRight(base.EscapedPath(), "/")
	reqPath := reqURL.EscapedPath()
	if basePath != "" {
		if reqPath != basePath && !strings.HasPrefix(reqPath, basePath+"/") {
			return fmt.Errorf("request path is outside configured Base URL path")
		}
	}
	return nil
}

func normalizePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
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

// IsHelpArg reports whether arg is a help request flag/subcommand.
func IsHelpArg(arg string) bool {
	switch arg {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

// LocalCommandArgs finds a branded local command before Restish parses argv.
// Leading verbosity/silence flags are ignored locally.
func LocalCommandArgs(args []string, name string) (commandArgs []string, handled bool, err error) {
	if len(args) < 2 {
		return nil, false, nil
	}
	if args[1] == "help" && len(args) >= 3 && args[2] == name {
		if len(args) >= 4 && args[3] == "login" {
			return []string{"login", "--help"}, true, nil
		}
		return []string{"--help"}, true, nil
	}
	for i := 1; i < len(args); i++ {
		if args[i] == "--" {
			return nil, false, nil
		}
		if args[i] == name {
			return args[i+1:], true, nil
		}
		if localDisplayFlag(args[i]) {
			continue
		}
		return nil, false, nil
	}
	return nil, false, nil
}

func localDisplayFlag(arg string) bool {
	if arg == "-S" || arg == "--rsh-silent" || strings.HasPrefix(arg, "--rsh-silent=") ||
		arg == "--rsh-verbose" || strings.HasPrefix(arg, "--rsh-verbose=") {
		return true
	}
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	for _, ch := range arg[1:] {
		if ch != 'v' {
			return false
		}
	}
	return true
}

// RedactSecrets masks common authorization values in diagnostic text.
func RedactSecrets(input string) string {
	if input == "" {
		return input
	}
	out := bearerPattern.ReplaceAllString(input, "Bearer ***")
	return secretKVPattern.ReplaceAllString(out, "${1}=***")
}
