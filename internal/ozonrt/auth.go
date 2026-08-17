package ozonrt

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/restishengine"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

type HeaderAuth struct {
	Session  Session
	Policy   *SafetyPolicy
	StateDir string
}

func (*HeaderAuth) Parameters() []restishauth.Param { return nil }

func (*HeaderAuth) SupportsForce() {}

func (a *HeaderAuth) Authenticate(_ context.Context, req *http.Request, ac restishauth.AuthContext) error {
	if err := authflow.RequestUnderBaseURL(req.URL, a.Session.BaseURL); err != nil {
		return fmt.Errorf("refusing to attach Ozon credentials: %w", err)
	}
	if a.Policy == nil {
		return fmt.Errorf("Ozon safety policy is unavailable")
	}
	if !a.Policy.Ready() {
		stateDir := a.StateDir
		if stateDir == "" {
			stateDir = ConfigDir()
		}
		a.Policy.PrimeFromSpecCache(os.Getenv("RSH_CACHE_DIR"), restishengine.ConfigPath(stateDir), "ozon")
	}
	if level, found := a.Policy.level(req.Method, req.URL.Path); ac.Force && found && level != "read" {
		return fmt.Errorf("Ozon %s request returned unauthorized; automatic retry is disabled for %s operations because the outcome is uncertain", strings.ToUpper(req.Method), level)
	}
	if err := a.Policy.Allow(req.Method, req.URL.Path, os.Getenv("OZON_WRITE_MODE")); err != nil {
		return err
	}
	if !a.Session.HasCredentials {
		return fmt.Errorf("Ozon authentication is not configured; run %q or set OZON_CLIENT_ID and OZON_API_KEY", "ozon auth login")
	}
	req.Header.Set("Client-Id", a.Session.Credentials.ClientID)
	req.Header.Set("Api-Key", a.Session.Credentials.APIKey)
	return nil
}

var apiKeyPattern = regexp.MustCompile(`(?i)\b((?:ozon[_-]?)?api[-_]?key)\s*[=:]\s*([^\s,;]+)`)

func RedactSecrets(input string) string {
	return apiKeyPattern.ReplaceAllString(authflow.RedactSecrets(input), "${1}=***")
}
