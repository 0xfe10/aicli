package fnsrt

import (
	"fmt"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
)

const (
	CredentialSourceEnvironment = authflow.SourceEnvironment
	CredentialSourceConfig      = authflow.SourceConfig
)

// Credentials is the resolved authorization material for API requests.
type Credentials struct {
	AccessToken string
	Source      string
}

type environmentSnapshot struct {
	BaseURL     string
	AccessToken string
	SpecURL     string
	Client      string
}

func readEnvironmentSnapshot() environmentSnapshot {
	return environmentSnapshot{
		BaseURL:     strings.TrimSpace(os.Getenv("FNS_BASE_URL")),
		AccessToken: strings.TrimSpace(os.Getenv("FNS_ACCESS_TOKEN")),
		SpecURL:     strings.TrimSpace(os.Getenv("FNS_SPEC_URL")),
		Client:      strings.TrimSpace(os.Getenv("FNS_CLIENT")),
	}
}

// ResolveCredentials applies environment-over-config precedence.
func ResolveCredentials() (Credentials, error) {
	return resolveCredentials(ConfigPath())
}

func resolveCredentials(configPath string) (Credentials, error) {
	env := readEnvironmentSnapshot()
	if creds, configured := credentialsFromSnapshot(FileConfig{}, env); configured {
		return creds, nil
	}
	file, err := LoadFileConfig(configPath)
	if err != nil {
		return Credentials{}, err
	}
	creds, configured := credentialsFromSnapshot(file, env)
	if configured {
		return creds, nil
	}
	return Credentials{}, fmt.Errorf("FNS authentication is not configured; run %q or set FNS_ACCESS_TOKEN", "fns auth login --mode token")
}

func credentialsFromSnapshot(file FileConfig, env environmentSnapshot) (Credentials, bool) {
	if env.AccessToken != "" {
		return Credentials{AccessToken: env.AccessToken, Source: CredentialSourceEnvironment}, true
	}
	if file.Auth == nil || strings.TrimSpace(file.Auth.AccessToken) == "" {
		return Credentials{}, false
	}
	return Credentials{AccessToken: file.Auth.AccessToken, Source: CredentialSourceConfig}, true
}

// ResolveBaseURL resolves Base URL with env > config > default precedence.
func ResolveBaseURL(configPath string) (value, source string, err error) {
	env := readEnvironmentSnapshot()
	if env.BaseURL != "" {
		return baseURLFromSnapshot(FileConfig{}, env)
	}
	file, err := LoadFileConfig(configPath)
	if err != nil {
		return "", "", err
	}
	return baseURLFromSnapshot(file, env)
}

func baseURLFromSnapshot(file FileConfig, env environmentSnapshot) (value, source string, err error) {
	if env.BaseURL != "" {
		normalized, err := authflow.NormalizeBaseURL(env.BaseURL)
		if err != nil {
			return "", "", fmt.Errorf("FNS_BASE_URL: %w", err)
		}
		return normalized, authflow.SourceEnvironment, nil
	}
	if file.BaseURL != "" {
		normalized, err := authflow.NormalizeBaseURL(file.BaseURL)
		if err != nil {
			return "", "", fmt.Errorf("config base_url: %w", err)
		}
		return normalized, authflow.SourceConfig, nil
	}
	return DefaultBaseURL, authflow.SourceDefault, nil
}

// IsPlaceholderBaseURL reports whether raw targets the compile-time example host.
// Matching is by canonical hostname and ignores case, trailing dots, ports, and paths.
func IsPlaceholderBaseURL(raw string) bool {
	host, err := authflow.HostnameOf(raw)
	if err != nil {
		host = authflow.CanonicalHostname(raw)
	}
	return host == PlaceholderHost
}

// RejectPlaceholderBaseURL fails closed before real API traffic to the example host.
func RejectPlaceholderBaseURL(raw string) error {
	if IsPlaceholderBaseURL(raw) {
		return fmt.Errorf("FNS Base URL is not configured; run %q or set FNS_BASE_URL", "fns auth login --mode token")
	}
	return nil
}

// HasUsableBaseURL reports whether baseURL can be used for real API requests.
func HasUsableBaseURL(baseURL, source string) bool {
	if strings.TrimSpace(baseURL) == "" {
		return false
	}
	if source == authflow.SourceDefault || IsPlaceholderBaseURL(baseURL) {
		return false
	}
	return true
}

// EnvironmentAuthPresent reports whether FNS_ACCESS_TOKEN is set.
func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("FNS_ACCESS_TOKEN")) != ""
}
