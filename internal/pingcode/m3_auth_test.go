package pingcode_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xfe10/aicli/internal/pingcode"
)

func TestPostDoesNotRetryOn401(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token" && r.URL.Query().Get("grant_type") == "client_credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.URL.Path == "/v1/auth/token" && r.URL.Query().Get("grant_type") == "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh", "expires_in": 3600})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/project/work_items":
			posts.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"expired"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	store := pingcode.NewAuthStore(filepath.Join(dir, "auth.json"))
	_, _ = store.Save(pingcode.StoredTokens{
		AccessToken:  "stale",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	})
	client := pingcode.NewClient(pingcode.Config{
		APIBaseURL: srv.URL,
		AuthScheme: "Bearer",
		TimeoutMS:  5000,
		ClientID:   "cid",
		ClientSecret: "csecret",
	}, store)
	_, err := client.CreateWorkItem(context.Background(), pingcode.WorkItemPayload{
		ProjectID: "p1", TypeID: "bug", Title: "t",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if posts.Load() != 1 {
		t.Fatalf("POST must not retry on 401, posts=%d", posts.Load())
	}
}

func TestGetRetriesOnceOn401(t *testing.T) {
	var gets atomic.Int32
	var refreshes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/token" && r.URL.Query().Get("grant_type") == "refresh_token":
			refreshes.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh", "expires_in": 3600})
		case r.URL.Path == "/v1/myself":
			n := gets.Add(1)
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "u1", "display_name": "Ada"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	store := pingcode.NewAuthStore(filepath.Join(dir, "auth.json"))
	_, _ = store.Save(pingcode.StoredTokens{
		AccessToken:  "stale",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	})
	client := pingcode.NewClient(pingcode.Config{APIBaseURL: srv.URL, AuthScheme: "Bearer", TimeoutMS: 5000}, store)
	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Ada" || gets.Load() != 2 || refreshes.Load() != 1 {
		t.Fatalf("gets=%d refreshes=%d user=%#v", gets.Load(), refreshes.Load(), user)
	}
}

func TestTokenFileRejectsOpenPermissionsAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"accessToken":"t","tokenType":"Bearer","savedAt":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := pingcode.NewAuthStore(path)
	if _, err := store.Get(); err == nil {
		t.Fatal("expected permissions error")
	}
	insp := pingcode.InspectTokenFile(path)
	if insp["status"] != "permissions_too_open" {
		t.Fatalf("inspect=%#v", insp)
	}

	badJSON := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badJSON, []byte(`{`), 0o600)
	store = pingcode.NewAuthStore(badJSON)
	if _, err := store.Get(); err == nil {
		t.Fatal("expected invalid json")
	}

	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skip("symlink not supported")
	}
	store = pingcode.NewAuthStore(link)
	if _, err := store.Get(); err == nil {
		t.Fatal("expected symlink reject")
	}

	missing := pingcode.InspectTokenFile(filepath.Join(dir, "nope.json"))
	if missing["status"] != "missing" || missing["ok"] != true {
		t.Fatalf("missing should be ok/not-logged-in: %#v", missing)
	}
}

func TestOAuthStateTTLAndOneTimeUse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth-state.json")
	orig := time.Now()
	pingcode.SetNowFunc(func() time.Time { return orig })
	t.Cleanup(func() { pingcode.SetNowFunc(time.Now) })

	if err := pingcode.SaveOAuthState(path, "abc"); err != nil {
		t.Fatal(err)
	}
	got, err := pingcode.LoadOAuthState(path)
	if err != nil || got.State != "abc" {
		t.Fatalf("got=%#v err=%v", got, err)
	}

	pingcode.SetNowFunc(func() time.Time { return orig.Add(11 * time.Minute) })
	if _, err := pingcode.LoadOAuthState(path); err == nil {
		t.Fatal("expected expiry")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired state should be cleared")
	}

	pingcode.SetNowFunc(func() time.Time { return orig })
	_ = pingcode.SaveOAuthState(path, "once")
	saved, err := pingcode.LoadOAuthState(path)
	if err != nil || saved.State != "once" {
		t.Fatal(err)
	}
	_ = pingcode.ClearOAuthState(path)
	if _, err := pingcode.LoadOAuthState(path); err == nil {
		t.Fatal("one-time state must fail after clear")
	}

	// Corrupt state
	_ = os.WriteFile(path, []byte(`{"state":"x"}`), 0o600)
	if _, err := pingcode.LoadOAuthState(path); err == nil {
		t.Fatal("missing CreatedAt must fail")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("corrupt state should be cleared")
	}
}
