package pingcodert

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	restishauth "github.com/rest-sh/restish/v2/auth"
)

type memoryTokenStore struct {
	mu     sync.Mutex
	tokens map[string]restishauth.CachedToken
}

func (s *memoryTokenStore) Get(key string) (*restishauth.CachedToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.tokens[key]
	if !ok {
		return nil, nil
	}
	return &token, nil
}

func (s *memoryTokenStore) Set(key string, token restishauth.CachedToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[key] = token
	return nil
}

func (s *memoryTokenStore) Delete(key string) error   { delete(s.tokens, key); return nil }
func (s *memoryTokenStore) DeletePrefix(string) error { return nil }

func TestClientCredentialsAuthCachesAndForceRefreshes(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Query().Get("client_secret"); got != "secret-value" {
			t.Fatalf("client_secret = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "token-" + string(rune('0'+calls)), "token_type": "Bearer", "expires_in": "3600"})
	}))
	defer server.Close()

	t.Setenv("PINGCODE_API_BASE_URL", server.URL)
	t.Setenv("PINGCODE_CLIENT_ID", "client-id")
	t.Setenv("PINGCODE_CLIENT_SECRET", "secret-value")
	t.Setenv("PINGCODE_WRITE_MODE", "readonly")
	store := &memoryTokenStore{tokens: map[string]restishauth.CachedToken{}}
	handler := &ClientCredentialsAuth{Session: Session{
		BaseURL:        server.URL,
		HasCredentials: true,
		Credentials: Credentials{
			Mode: AuthModeClient, ClientID: "client-id", ClientSecret: "secret-value",
			Source: CredentialSourceEnvironment,
		},
	}}
	authContext := restishauth.AuthContext{BaseURL: server.URL, CacheKey: "test", TokenStore: store, HTTPClient: server.Client()}

	for _, force := range []bool{false, false, true} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/resource", nil)
		authContext.Force = force
		if err := handler.Authenticate(context.Background(), req, authContext); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer token-") {
			t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
		}
	}
	if calls != 2 {
		t.Fatalf("token endpoint calls = %d, want 2", calls)
	}
}

func TestClientCredentialsAuthWritePolicy(t *testing.T) {
	handler := &ClientCredentialsAuth{Session: Session{
		BaseURL:        "https://open.pingcode.com",
		HasCredentials: true,
		Credentials:    Credentials{Mode: AuthModeToken, AccessToken: "token", Source: CredentialSourceEnvironment},
	}}
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
			t.Setenv("PINGCODE_WRITE_MODE", test.mode)
			req, _ := http.NewRequest(test.method, "https://open.pingcode.com/v1/test", nil)
			err := handler.Authenticate(context.Background(), req, restishauth.AuthContext{BaseURL: "https://open.pingcode.com"})
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestCachedTokenSafetyMargin(t *testing.T) {
	cacheKey := clientCredentialsCacheKey("test", DefaultAPIBaseURL, "unused", "unused")
	store := &memoryTokenStore{tokens: map[string]restishauth.CachedToken{
		cacheKey: {AccessToken: "cached", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)},
	}}
	t.Setenv("PINGCODE_CLIENT_ID", "unused")
	t.Setenv("PINGCODE_CLIENT_SECRET", "unused")
	req, _ := http.NewRequest(http.MethodGet, "https://open.pingcode.com/v1/test", nil)
	err := (&ClientCredentialsAuth{Session: Session{
		BaseURL:        DefaultAPIBaseURL,
		HasCredentials: true,
		Credentials: Credentials{
			Mode: AuthModeClient, ClientID: "unused", ClientSecret: "unused",
		},
	}}).Authenticate(context.Background(), req, restishauth.AuthContext{CacheKey: "test", TokenStore: store, BaseURL: DefaultAPIBaseURL})
	if err != nil || req.Header.Get("Authorization") != "Bearer cached" {
		t.Fatalf("authorization=%q error=%v", req.Header.Get("Authorization"), err)
	}
}

func TestClientCredentialsCacheKeySeparatesAPIHosts(t *testing.T) {
	one := clientCredentialsCacheKey("pingcode:default", "https://one.example.com/", "client-one", "secret")
	two := clientCredentialsCacheKey("pingcode:default", "https://two.example.com", "client-one", "secret")
	if one == two {
		t.Fatalf("cache keys should differ across API hosts: %q", one)
	}
	if got := clientCredentialsCacheKey("pingcode:default", "https://one.example.com", "client-one", "secret"); got != one {
		t.Fatalf("cache key is not stable: %q != %q", got, one)
	}
}

func TestClientCredentialsCacheKeySeparatesClientIDs(t *testing.T) {
	one := clientCredentialsCacheKey("pingcode:default", DefaultAPIBaseURL, "client-one", "secret")
	two := clientCredentialsCacheKey("pingcode:default", DefaultAPIBaseURL, "client-two", "secret")
	if one == two {
		t.Fatalf("cache keys should differ across client IDs: %q", one)
	}
}

func TestClientCredentialsCacheKeyChangesWithSecret(t *testing.T) {
	one := clientCredentialsCacheKey("pingcode:default", DefaultAPIBaseURL, "client", "secret-one")
	two := clientCredentialsCacheKey("pingcode:default", DefaultAPIBaseURL, "client", "secret-two")
	if one == two || strings.Contains(one, "secret-one") || strings.Contains(two, "secret-two") {
		t.Fatalf("cache keys must use a non-plaintext secret fingerprint: %q %q", one, two)
	}
}

func TestTokenTransportErrorDoesNotLeakClientSecret(t *testing.T) {
	secret := "must-not-leak"
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("request failed for " + req.URL.String())
	})}
	_, err := fetchClientCredentialsToken(context.Background(), client, DefaultAPIBaseURL, "client", secret)
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "client_secret") {
		t.Fatalf("transport error leaked credentials: %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }
