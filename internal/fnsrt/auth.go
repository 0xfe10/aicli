package fnsrt

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	restishauth "github.com/rest-sh/restish/v2/auth"
)

// BearerAuth attaches the local FNS access token and enforces write-mode gates.
type BearerAuth struct{}

func (*BearerAuth) Parameters() []restishauth.Param { return nil }

func (*BearerAuth) SupportsForce() {}

func (*BearerAuth) Authenticate(_ context.Context, req *http.Request, ac restishauth.AuthContext) error {
	if err := RejectPlaceholderBaseURL(ac.BaseURL); err != nil {
		return err
	}
	if err := enforceWriteMode(req.Method, os.Getenv("FNS_WRITE_MODE")); err != nil {
		return err
	}
	if ac.Force && isWriteMethod(req.Method) {
		return fmt.Errorf("FNS %s request returned unauthorized; automatic retry is disabled for writes because the outcome is uncertain", strings.ToUpper(req.Method))
	}

	creds, err := ResolveCredentials()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
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
