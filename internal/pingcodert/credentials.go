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

// ResolveCredentials applies the environment-over-config precedence rules.
func ResolveCredentials() (Credentials, error) {
	return resolveCredentials(ConfigPath())
}

func resolveCredentials(configPath string) (Credentials, error) {
	token := strings.TrimSpace(os.Getenv("PINGCODE_ACCESS_TOKEN"))
	clientID := strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_SECRET"))

	if token != "" {
		return Credentials{
			Mode:        AuthModeToken,
			AccessToken: token,
			Source:      CredentialSourceEnvironment,
		}, nil
	}
	if clientID != "" || clientSecret != "" {
		if clientID == "" || clientSecret == "" {
			return Credentials{}, fmt.Errorf("PingCode authentication requires both PINGCODE_CLIENT_ID and PINGCODE_CLIENT_SECRET when using environment client credentials")
		}
		return Credentials{
			Mode:         AuthModeClient,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Source:       CredentialSourceEnvironment,
		}, nil
	}

	auth, err := LoadAuthConfig(configPath)
	if err != nil {
		return Credentials{}, err
	}
	if auth == nil {
		return Credentials{}, fmt.Errorf("PingCode authentication is not configured; run %q or set PINGCODE_ACCESS_TOKEN / PINGCODE_CLIENT_ID and PINGCODE_CLIENT_SECRET", "pingcode auth login")
	}
	switch auth.Mode {
	case AuthModeToken:
		return Credentials{
			Mode:        AuthModeToken,
			AccessToken: auth.AccessToken,
			Source:      CredentialSourceConfig,
		}, nil
	case AuthModeClient:
		return Credentials{
			Mode:         AuthModeClient,
			ClientID:     auth.ClientID,
			ClientSecret: auth.ClientSecret,
			Source:       CredentialSourceConfig,
		}, nil
	default:
		return Credentials{}, fmt.Errorf("unsupported auth mode %q in PingCode config", auth.Mode)
	}
}

// ResolveBaseURL resolves Base URL with env > config > default precedence.
func ResolveBaseURL(configPath string) (value, source string, err error) {
	if env := strings.TrimSpace(os.Getenv("PINGCODE_API_BASE_URL")); env != "" {
		normalized, err := authflow.NormalizeBaseURL(env)
		if err != nil {
			return "", "", fmt.Errorf("PINGCODE_API_BASE_URL: %w", err)
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
	return DefaultAPIBaseURL, authflow.SourceDefault, nil
}

// EnvironmentAuthPresent reports whether any auth-related environment variable is set.
func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("PINGCODE_ACCESS_TOKEN")) != "" ||
		strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_ID")) != "" ||
		strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_SECRET")) != ""
}
