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

// ResolveCredentials applies environment-over-config precedence.
func ResolveCredentials() (Credentials, error) {
	return resolveCredentials(ConfigPath())
}

func resolveCredentials(configPath string) (Credentials, error) {
	if token := strings.TrimSpace(os.Getenv("FNS_ACCESS_TOKEN")); token != "" {
		return Credentials{AccessToken: token, Source: CredentialSourceEnvironment}, nil
	}
	file, err := LoadFileConfig(configPath)
	if err != nil {
		return Credentials{}, err
	}
	if file.Auth == nil || strings.TrimSpace(file.Auth.AccessToken) == "" {
		return Credentials{}, fmt.Errorf("FNS authentication is not configured; run %q or set FNS_ACCESS_TOKEN", "fns auth login --mode token")
	}
	return Credentials{AccessToken: file.Auth.AccessToken, Source: CredentialSourceConfig}, nil
}

// ResolveBaseURL resolves Base URL with env > config > default precedence.
func ResolveBaseURL(configPath string) (value, source string, err error) {
	if env := strings.TrimSpace(os.Getenv("FNS_BASE_URL")); env != "" {
		normalized, err := authflow.NormalizeBaseURL(env)
		if err != nil {
			return "", "", fmt.Errorf("FNS_BASE_URL: %w", err)
		}
		return normalized, authflow.SourceEnvironment, nil
	}
	file, err := LoadFileConfig(configPath)
	if err != nil {
		return "", "", err
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

// IsPlaceholderBaseURL reports whether url is the compile-time example host.
func IsPlaceholderBaseURL(raw string) bool {
	normalized, err := authflow.NormalizeBaseURL(raw)
	if err != nil {
		return strings.TrimRight(strings.TrimSpace(raw), "/") == strings.TrimRight(DefaultBaseURL, "/")
	}
	return normalized == strings.TrimRight(DefaultBaseURL, "/")
}

// RejectPlaceholderBaseURL fails closed before real API traffic to the example host.
func RejectPlaceholderBaseURL(raw string) error {
	if IsPlaceholderBaseURL(raw) {
		return fmt.Errorf("FNS Base URL is not configured; run %q or set FNS_BASE_URL", "fns auth login --mode token")
	}
	return nil
}

// EnvironmentAuthPresent reports whether FNS_ACCESS_TOKEN is set.
func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("FNS_ACCESS_TOKEN")) != ""
}
