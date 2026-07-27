package pingcodert

import (
	"fmt"
	"os"
	"strings"
)

const (
	CredentialSourceEnvironment = "environment"
	CredentialSourceConfig      = "config"
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

// EnvironmentAuthPresent reports whether any auth-related environment variable is set.
func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("PINGCODE_ACCESS_TOKEN")) != "" ||
		strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_ID")) != "" ||
		strings.TrimSpace(os.Getenv("PINGCODE_CLIENT_SECRET")) != ""
}
