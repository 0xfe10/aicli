package fnsrt

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

// BearerAuth attaches the local FNS access token and enforces write-mode gates.
// Session must be bound at CLI construction so Base URL and credentials stay
// consistent for the process lifetime.
type BearerAuth struct {
	Session Session
}

func (*BearerAuth) Parameters() []restishauth.Param { return nil }

func (*BearerAuth) SupportsForce() {}

func (a *BearerAuth) Authenticate(_ context.Context, req *http.Request, ac restishauth.AuthContext) error {
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
			return fmt.Errorf("refusing to attach FNS credentials: %w", err)
		}
	}
	if err := enforceWriteMode(req.Method, os.Getenv("FNS_WRITE_MODE")); err != nil {
		return err
	}
	if ac.Force && isWriteMethod(req.Method) {
		return fmt.Errorf("FNS %s request returned unauthorized; automatic retry is disabled for writes because the outcome is uncertain", strings.ToUpper(req.Method))
	}

	if !a.Session.HasCredentials || strings.TrimSpace(a.Session.Credentials.AccessToken) == "" {
		return fmt.Errorf("FNS authentication is not configured; run %q or set FNS_ACCESS_TOKEN", "fns auth login --mode token")
	}
	req.Header.Set("Authorization", "Bearer "+a.Session.Credentials.AccessToken)
	return nil
}

func enforceWriteMode(method, rawMode string) error {
	mode := strings.ToLower(strings.TrimSpace(rawMode))
	if mode == "" {
		mode = "readonly"
	}
	method = strings.ToUpper(method)
	readMethod := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
	writeMethod := method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
	deleteMethod := method == http.MethodDelete

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
		return fmt.Errorf("invalid FNS_WRITE_MODE %q: expected readonly, write, or destructive", rawMode)
	}
	return fmt.Errorf("FNS %s request is blocked by FNS_WRITE_MODE=%s", method, mode)
}

func isWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
