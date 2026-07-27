package fnsrt

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
	if strings.TrimSpace(file.AccessToken) == "" {
		return Credentials{}, fmt.Errorf("FNS authentication is not configured; run %q or set FNS_ACCESS_TOKEN", "fns auth login --mode token")
	}
	return Credentials{AccessToken: file.AccessToken, Source: CredentialSourceConfig}, nil
}

// EnvironmentAuthPresent reports whether FNS_ACCESS_TOKEN is set.
func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("FNS_ACCESS_TOKEN")) != ""
}
