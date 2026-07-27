package fnsrt

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	restishauth "github.com/rest-sh/restish/v2/auth"
)

func TestBearerAuthWritePolicy(t *testing.T) {
	handler := &BearerAuth{}
	for _, test := range []struct {
		mode, method string
		wantError    bool
	}{
		{"", http.MethodPost, true},
		{"readonly", http.MethodGet, false},
		{"write", http.MethodPatch, false},
		{"write", http.MethodDelete, true},
		{"destructive", http.MethodDelete, false},
		{"invalid", http.MethodGet, true},
	} {
		t.Run(test.mode+test.method, func(t *testing.T) {
			t.Setenv("FNS_ACCESS_TOKEN", "token")
			t.Setenv("FNS_WRITE_MODE", test.mode)
			req, _ := http.NewRequest(test.method, "https://obsidian-fns.kahub.in/api/note", nil)
			err := handler.Authenticate(context.Background(), req, restishauth.AuthContext{})
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError=%v", err, test.wantError)
			}
			if err == nil && req.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
			}
		})
	}
}

func TestBearerAuthRejectsWriteForceRetry(t *testing.T) {
	t.Setenv("FNS_ACCESS_TOKEN", "token")
	t.Setenv("FNS_WRITE_MODE", "write")
	req, _ := http.NewRequest(http.MethodPost, "https://obsidian-fns.kahub.in/api/note", nil)
	err := (&BearerAuth{}).Authenticate(context.Background(), req, restishauth.AuthContext{Force: true})
	if err == nil || !strings.Contains(err.Error(), "automatic retry is disabled for writes") {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthConfigPermissionsAndStatus(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("FNS_ACCESS_TOKEN", "")

	var stdout, stderr bytes.Buffer
	authIO := AuthIO{
		Stdout: &stdout,
		Stderr: &stderr,
		ReadSecret: func(string) (string, error) {
			return "super-secret-token", nil
		},
	}
	if err := RunAuth([]string{"login", "--mode", "token"}, authIO); err != nil {
		t.Fatal(err)
	}
	path := ConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions = %v", info.Mode())
	}
	stdout.Reset()
	if err := RunAuth([]string{"status"}, authIO); err != nil {
		t.Fatal(err)
	}
	status := stdout.String()
	if strings.Contains(status, "super-secret-token") {
		t.Fatalf("status leaked token: %s", status)
	}
	if !strings.Contains(status, `"configured": true`) || !strings.Contains(status, `"hasToken": true`) {
		t.Fatalf("status = %s", status)
	}

	creds, err := ResolveCredentials()
	if err != nil || creds.AccessToken != "super-secret-token" || creds.Source != CredentialSourceConfig {
		t.Fatalf("creds=%#v err=%v", creds, err)
	}

	t.Setenv("FNS_ACCESS_TOKEN", "env-token")
	creds, err = ResolveCredentials()
	if err != nil || creds.AccessToken != "env-token" || creds.Source != CredentialSourceEnvironment {
		t.Fatalf("env creds=%#v err=%v", creds, err)
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("FNS_ACCESS_TOKEN", "")
	if err := RunAuth([]string{"logout"}, authIO); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFileConfig(path)
	if err != nil || file.AccessToken != "" {
		t.Fatalf("logout left token: %#v err=%v", file, err)
	}
}

func TestRedactSecrets(t *testing.T) {
	in := `Authorization: Bearer abc.def.ghi access_token=xyz token: secret`
	out := RedactSecrets(in)
	if strings.Contains(out, "abc.def.ghi") || strings.Contains(out, "xyz") || strings.Contains(out, "secret") {
		t.Fatalf("not redacted: %s", out)
	}
	if !strings.Contains(out, "Bearer ***") || !strings.Contains(out, "access_token=***") {
		t.Fatalf("unexpected redaction: %s", out)
	}
}

func TestAuthRejectsArgvSecrets(t *testing.T) {
	err := RunAuth([]string{"login", "--mode", "token", "--access-token", "x"}, AuthIO{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v", err)
	}
}
