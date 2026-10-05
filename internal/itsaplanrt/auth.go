package itsaplanrt

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

// HeaderAuth attaches the local ITSAPLAN access token and enforces write-mode gates.
// Session must be bound at CLI construction so Base URL and credentials stay
// consistent for the process lifetime.
type HeaderAuth struct {
	Session Session
}

func (*HeaderAuth) Parameters() []restishauth.Param { return nil }

func (*HeaderAuth) SupportsForce() {}

func (a *HeaderAuth) Authenticate(_ context.Context, req *http.Request, ac restishauth.AuthContext) error {
	baseURL := strings.TrimSpace(a.Session.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(ac.BaseURL)
	}
	if err := RejectPlaceholderBaseURL(baseURL); err != nil {
		return err
	}
	if req != nil {
		if err := RejectPlaceholderBaseURL(req.URL.String()); err != nil {
			return err
		}
		if err := authflow.RequestUnderBaseURL(req.URL, baseURL); err != nil {
			return fmt.Errorf("refusing to attach ITSAPLAN credentials: %w", err)
		}
	}
	if err := enforceWriteMode(req.Method, req.URL.Path, os.Getenv("ITSAPLAN_WRITE_MODE")); err != nil {
		return err
	}
	if ac.Force && isWriteMethod(req.Method) {
		return fmt.Errorf("ITSAPLAN %s request returned unauthorized; automatic retry is disabled for writes because the outcome is uncertain", strings.ToUpper(req.Method))
	}

	if !a.Session.HasCredentials || strings.TrimSpace(a.Session.Credentials.APIKey) == "" {
		return fmt.Errorf("ITSAPLAN authentication is not configured; run %q or set ITSAPLAN_API_KEY", "itsaplan auth login --mode key")
	}
	req.Header.Set("x-api-key", a.Session.Credentials.APIKey)
	return nil
}

func enforceWriteMode(method, path, rawMode string) error {
	mode := strings.ToLower(strings.TrimSpace(rawMode))
	if mode == "" {
		mode = "readonly"
	}
	method = strings.ToUpper(method)
	readMethod := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	writeMethod := method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
	deleteMethod := method == http.MethodDelete || destructivePath(path)
	if deleteMethod {
		readMethod = false
		writeMethod = false
	}

	switch mode {
	case "readonly":
		if readMethod {
			return nil
		}
	case "write":
		if readMethod || writeMethod {
			return nil
		}
	case "destructive":
		if readMethod || writeMethod || deleteMethod {
			return nil
		}
	default:
		return fmt.Errorf("invalid ITSAPLAN_WRITE_MODE %q: expected readonly, write, or destructive", rawMode)
	}
	return fmt.Errorf("ITSAPLAN %s request is blocked by ITSAPLAN_WRITE_MODE=%s", method, mode)
}

func isWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
