package lanhurt

import (
	"context"
	"fmt"
	"net/http"

	restishauth "github.com/rest-sh/restish/v2/auth"
)

const AuthType = "lanhu-cookie"

type HeaderAuth struct{ Session Session }

func (*HeaderAuth) Parameters() []restishauth.Param { return nil }
func (*HeaderAuth) SupportsForce()                  {}

func (a *HeaderAuth) Authenticate(_ context.Context, req *http.Request, _ restishauth.AuthContext) error {
	if !a.Session.HasCredentials {
		return fmt.Errorf("Lanhu authentication is not configured; run %q or set LANHU_COOKIE", "lanhu auth login --mode cookie")
	}
	switch req.URL.Hostname() {
	case "lanhuapp.com":
		req.Header.Set("Cookie", a.Session.Cookie)
		req.Header.Set("Referer", "https://lanhuapp.com/web/")
		req.Header.Set("request-from", "web")
	case "dds.lanhuapp.com":
		req.Header.Set("Cookie", a.Session.DDSCookie)
		req.Header.Set("Referer", "https://dds.lanhuapp.com/")
		req.Header.Set("Authorization", "Basic dW5kZWZpbmVkOg==")
	default:
		return fmt.Errorf("refusing to attach Lanhu credentials to %q", req.URL.Hostname())
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	return nil
}
