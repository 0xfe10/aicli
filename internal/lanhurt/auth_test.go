package lanhurt

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xfe10/aicli/internal/contextflow"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestConfigRoundTripEnvironmentAndLogout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aicli", "lanhu", "config.toml")
	if err := saveLogin(path, "lanhu-file", "dds-file"); err != nil {
		t.Fatal(err)
	}
	selection := contextflow.Selection{ConfigDir: filepath.Dir(path)}
	session, err := LoadSessionWithContext(selection)
	if err != nil || session.Cookie != "lanhu-file" || session.DDSCookie != "dds-file" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%v err=%v", info.Mode().Perm(), err)
	}
	t.Setenv("LANHU_COOKIE", "lanhu-env")
	t.Setenv("DDS_COOKIE", "dds-env")
	session, err = LoadSessionWithContext(selection)
	if err != nil || session.Cookie != "lanhu-env" || session.DDSCookie != "dds-env" {
		t.Fatalf("environment session=%+v err=%v", session, err)
	}
	if err := clearLogin(path); err != nil {
		t.Fatal(err)
	}
}

func TestClientStripsCookieOnSignedHostRedirect(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "lanhuapp.com" {
			if req.Header.Get("Cookie") != "secret" {
				t.Fatalf("initial Cookie=%q", req.Header.Get("Cookie"))
			}
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://cdn.lanhuapp.com/value.json"}}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
		}
		if req.Header.Get("Cookie") != "" {
			t.Fatalf("redirect leaked Cookie=%q", req.Header.Get("Cookie"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewBufferString(`{"ok":true}`)), Request: req}, nil
	})
	client := Client{Cookie: "secret", HTTP: &http.Client{Transport: transport}}
	var result map[string]any
	if err := client.get(context.Background(), "https://lanhuapp.com/start", &result); err != nil {
		t.Fatal(err)
	}
}

func TestClientDoesNotAttachCookiesToNonStandardOrigins(t *testing.T) {
	client := Client{Cookie: "lanhu", DDSCookie: "dds"}
	for _, raw := range []string{"http://lanhuapp.com/api", "https://lanhuapp.com:8443/api", "http://dds.lanhuapp.com/api", "https://dds.lanhuapp.com:8443/api"} {
		target, _ := url.Parse(raw)
		req := &http.Request{URL: target, Header: http.Header{}}
		client.applyHeaders(req)
		if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("credentials attached to %s", raw)
		}
	}
}

func TestHeaderAuthScopesCookiesByExactHost(t *testing.T) {
	auth := HeaderAuth{Session: Session{Cookie: "lanhu", DDSCookie: "dds", HasCredentials: true}}
	for _, test := range []struct {
		host, want string
	}{
		{"lanhuapp.com", "lanhu"},
		{"dds.lanhuapp.com", "dds"},
	} {
		req := &http.Request{URL: &url.URL{Scheme: "https", Host: test.host}, Header: http.Header{}}
		if err := auth.Authenticate(context.Background(), req, restishauth.AuthContext{}); err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Cookie"); got != test.want {
			t.Fatalf("%s Cookie=%q want %q", test.host, got, test.want)
		}
	}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "evil.example"}, Header: http.Header{}}
	if auth.Authenticate(context.Background(), req, restishauth.AuthContext{}) == nil {
		t.Fatal("credentials were allowed on an unrelated host")
	}
	for _, raw := range []string{"http://lanhuapp.com", "https://lanhuapp.com:8443", "http://dds.lanhuapp.com"} {
		target, _ := url.Parse(raw)
		req := &http.Request{URL: target, Header: http.Header{}}
		if auth.Authenticate(context.Background(), req, restishauth.AuthContext{}) == nil || req.Header.Get("Cookie") != "" {
			t.Fatalf("credentials allowed for %s", raw)
		}
	}
}
