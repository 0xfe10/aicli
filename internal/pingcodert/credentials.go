package pingcodert

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
	Mode         string
	ClientID     string
	ClientSecret string
	AccessToken  string
	Source       string
}

type environmentSnapshot struct {
	BaseURL      string
	SpecURL      string
	AccessToken  string
	ClientID     string
	ClientSecret string
}

func readEnvironmentSnapshot() environmentSnapshot {
	return environmentSnapshot{
		BaseURL:      strings.TrimSpace(os.Getenv("PINGCODE_API_BASE_URL")),
		SpecURL:      strings.TrimSpace(os.Getenv("PINGCODE_SPEC_URL")),
		AccessToken:  strings.TrimSpace(os.Getenv("PINGCODE_ACCESS_TOKEN")),
		ClientID:     strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_SECRET")),
	}
}

// ResolveCredentials applies the environment-over-config precedence rules.
func ResolveCredentials() (Credentials, error) {
	return resolveCredentials(ConfigPath())
}

func resolveCredentials(configPath string) (Credentials, error) {
	env := readEnvironmentSnapshot()
	if env.AccessToken != "" || env.ClientID != "" || env.ClientSecret != "" {
		creds, configured, err := credentialsFromSnapshot(FileConfig{}, env)
		if err != nil {
			return Credentials{}, err
		}
		if configured {
			return creds, nil
		}
	}
	file, err := LoadFileConfig(configPath)
	if err != nil {
		return Credentials{}, err
	}
	creds, configured, err := credentialsFromSnapshot(file, env)
	if err != nil {
		return Credentials{}, err
	}
	if !configured {
		return Credentials{}, fmt.Errorf("PingCode authentication is not configured; run %q or set PINGCODE_ACCESS_TOKEN / PINGCODE_CLIENT_ID and PINGCODE_CLIENT_SECRET", "pingcode auth login")
	}
	return creds, nil
}

func credentialsFromSnapshot(file FileConfig, env environmentSnapshot) (Credentials, bool, error) {
	if env.AccessToken != "" {
		return Credentials{Mode: AuthModeToken, AccessToken: env.AccessToken, Source: CredentialSourceEnvironment}, true, nil
	}
	if env.ClientID != "" || env.ClientSecret != "" {
		if env.ClientID == "" || env.ClientSecret == "" {
			return Credentials{}, false, fmt.Errorf("PingCode authentication requires both PINGCODE_CLIENT_ID and PINGCODE_CLIENT_SECRET when using environment client credentials")
		}
		return Credentials{Mode: AuthModeClient, ClientID: env.ClientID, ClientSecret: env.ClientSecret, Source: CredentialSourceEnvironment}, true, nil
	}
	auth := file.Auth
	if auth == nil {
		return Credentials{}, false, nil
	}
	switch auth.Mode {
	case AuthModeToken:
		return Credentials{
			Mode:        AuthModeToken,
			AccessToken: auth.AccessToken,
			Source:      CredentialSourceConfig,
		}, true, nil
	case AuthModeClient:
		return Credentials{
			Mode:         AuthModeClient,
			ClientID:     auth.ClientID,
			ClientSecret: auth.ClientSecret,
			Source:       CredentialSourceConfig,
		}, true, nil
	default:
		return Credentials{}, false, fmt.Errorf("unsupported auth mode %q in PingCode config", auth.Mode)
	}
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
			return "", "", fmt.Errorf("PINGCODE_API_BASE_URL: %w", err)
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
	return DefaultAPIBaseURL, authflow.SourceDefault, nil
}

// EnvironmentAuthPresent reports whether any auth-related environment variable is set.
func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("PINGCODE_ACCESS_TOKEN")) != "" ||
		strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_ID")) != "" ||
		strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_SECRET")) != ""
}
