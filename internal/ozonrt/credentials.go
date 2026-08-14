package ozonrt

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

type Credentials struct {
	ClientID string
	APIKey   string
	Source   string
}

type environmentSnapshot struct {
	BaseURL  string
	SpecURL  string
	ClientID string
	APIKey   string
}

func readEnvironmentSnapshot() environmentSnapshot {
	return environmentSnapshot{
		BaseURL:  strings.TrimSpace(os.Getenv("OZON_BASE_URL")),
		SpecURL:  strings.TrimSpace(os.Getenv("OZON_SPEC_URL")),
		ClientID: strings.TrimSpace(os.Getenv("OZON_CLIENT_ID")),
		APIKey:   strings.TrimSpace(os.Getenv("OZON_API_KEY")),
	}
}

func resolveCredentials(file FileConfig, env environmentSnapshot) (Credentials, bool, error) {
	if env.ClientID != "" || env.APIKey != "" {
		if env.ClientID == "" || env.APIKey == "" {
			return Credentials{}, false, fmt.Errorf("OZON_CLIENT_ID and OZON_API_KEY must be set together")
		}
		return Credentials{ClientID: env.ClientID, APIKey: env.APIKey, Source: CredentialSourceEnvironment}, true, nil
	}
	if file.Auth == nil {
		return Credentials{}, false, nil
	}
	return Credentials{ClientID: file.Auth.ClientID, APIKey: file.Auth.APIKey, Source: CredentialSourceConfig}, true, nil
}

func resolveBaseURL(file FileConfig, env environmentSnapshot) (string, string, error) {
	value, source := DefaultBaseURL, authflow.SourceDefault
	if file.BaseURL != "" {
		value, source = file.BaseURL, authflow.SourceConfig
	}
	if env.BaseURL != "" {
		value, source = env.BaseURL, authflow.SourceEnvironment
	}
	normalized, err := authflow.NormalizeBaseURL(value)
	if err != nil {
		return "", "", err
	}
	return normalized, source, nil
}

func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("OZON_CLIENT_ID")) != "" || strings.TrimSpace(os.Getenv("OZON_API_KEY")) != ""
}
