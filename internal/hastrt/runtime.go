// Package hastrt connects Hast MCP with local auth and contexts.
package hastrt

import (
	"fmt"
	"github.com/0xfe10/aicli/internal/contextflow"
	"net/url"
)

const DefaultBaseURL = "https://backend.hast.so"
const PlaceholderHost = "hast.example.com"

type Config struct{ BaseURL string }
type Session struct {
	BaseURL, BaseURLSource string
	Credentials            Credentials
	HasCredentials         bool
	CredentialSource       string
}

func LoadSessionWithContext(selection contextflow.Selection) (Session, Config, error) {
	var file FileConfig
	var err error
	if path := ConfigPathFor(selection); path != "" {
		file, err = LoadFileConfig(path)
	}
	if err != nil {
		return Session{}, Config{}, err
	}
	env := readEnvironmentSnapshot()
	base, source, err := baseURLFromSnapshot(file, env)
	if err != nil {
		return Session{}, Config{}, err
	}
	// Strict origin isolation: same-origin redirects cannot enforce subpath scope.
	if err := validateOrigin(base); err != nil {
		return Session{}, Config{}, err
	}
	session := Session{BaseURL: base, BaseURLSource: source}
	if creds, ok := credentialsFromSnapshot(file, env); ok {
		session.Credentials = creds
		session.HasCredentials = true
		session.CredentialSource = creds.Source
	}
	return session, Config{BaseURL: base}, nil
}

func validateOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid API origin")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("hast Base URL must be an origin without a path")
	}
	return nil
}
