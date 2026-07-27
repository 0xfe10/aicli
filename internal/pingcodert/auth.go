package pingcodert

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	restishauth "github.com/rest-sh/restish/v2/auth"
)

const maxTokenResponseBytes = 1 << 20

// ClientCredentialsAuth implements PingCode's non-standard GET-based client
// credentials exchange and participates in Restish's token cache and 401 retry.
type ClientCredentialsAuth struct{}

func (*ClientCredentialsAuth) Parameters() []restishauth.Param { return nil }

func (*ClientCredentialsAuth) SupportsForce() {}

func (*ClientCredentialsAuth) Authenticate(ctx context.Context, req *http.Request, ac restishauth.AuthContext) error {
	if err := enforceWriteMode(req.Method, os.Getenv("PINGCODE_WRITE_MODE")); err != nil {
		return err
	}
	if ac.Force && isWriteMethod(req.Method) {
		return fmt.Errorf("PingCode %s request returned unauthorized; automatic retry is disabled for writes because the outcome is uncertain", strings.ToUpper(req.Method))
	}

	creds, err := ResolveCredentials()
	if err != nil {
		return err
	}
	if creds.Mode == AuthModeToken {
		req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
		return nil
	}

	baseURL := strings.TrimRight(envOr("PINGCODE_API_BASE_URL", ac.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	cacheKey := clientCredentialsCacheKey(ac.CacheKey, baseURL, creds.ClientID)
	if !ac.Force && ac.TokenStore != nil {
		cached, err := ac.TokenStore.Get(cacheKey)
		if err != nil {
			return fmt.Errorf("read PingCode token cache: %w", err)
		}
		if cached != nil && cached.AccessToken != "" && !cached.IsExpired() {
			req.Header.Set("Authorization", tokenType(cached.TokenType)+" "+cached.AccessToken)
			return nil
		}
	}

	token, err := fetchClientCredentialsToken(ctx, ac.HTTPClient, baseURL, creds.ClientID, creds.ClientSecret)
	if err != nil {
		return err
	}
	if ac.TokenStore != nil {
		if err := ac.TokenStore.Set(cacheKey, token); err != nil {
			return fmt.Errorf("write PingCode token cache: %w", err)
		}
	}
	req.Header.Set("Authorization", tokenType(token.TokenType)+" "+token.AccessToken)
	return nil
}

func clientCredentialsCacheKey(baseKey, baseURL, clientID string) string {
	if baseKey == "" {
		baseKey = "pingcode/default/client-credentials"
	}
	baseURLSum := sha256.Sum256([]byte(strings.TrimRight(baseURL, "/")))
	clientIDSum := sha256.Sum256([]byte(strings.TrimSpace(clientID)))
	return fmt.Sprintf("%s:cred:client:base_url:%x:client_id:%x", baseKey, baseURLSum[:8], clientIDSum[:8])
}

func fetchClientCredentialsToken(ctx context.Context, client *http.Client, baseURL, clientID, clientSecret string) (restishauth.CachedToken, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/v1/auth/token")
	if err != nil {
		return restishauth.CachedToken{}, fmt.Errorf("build PingCode token URL: %w", err)
	}
	query := u.Query()
	query.Set("grant_type", "client_credentials")
	query.Set("client_id", clientID)
	query.Set("client_secret", clientSecret)
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return restishauth.CachedToken{}, fmt.Errorf("build PingCode token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return restishauth.CachedToken{}, fmt.Errorf("request PingCode token: transport failure")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if err != nil {
		return restishauth.CachedToken{}, fmt.Errorf("read PingCode token response: %w", err)
	}
	if len(body) > maxTokenResponseBytes {
		return restishauth.CachedToken{}, fmt.Errorf("PingCode token response exceeds %d bytes", maxTokenResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return restishauth.CachedToken{}, fmt.Errorf("PingCode token endpoint returned HTTP %d", resp.StatusCode)
	}
	var payload tokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return restishauth.CachedToken{}, fmt.Errorf("decode PingCode token response: %w", err)
	}
	if payload.AccessToken == "" {
		return restishauth.CachedToken{}, fmt.Errorf("PingCode token response is missing access_token")
	}
	expiresIn := payload.ExpiresIn.Duration()
	if expiresIn <= 0 {
		expiresIn = 2 * time.Hour
	}
	if expiresIn > time.Minute {
		expiresIn -= time.Minute
	}
	return restishauth.CachedToken{
		AccessToken: payload.AccessToken,
		TokenType:   tokenType(payload.TokenType),
		Expiry:      time.Now().Add(expiresIn),
	}, nil
}

type tokenResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresIn   secondsValue `json:"expires_in"`
}

type secondsValue time.Duration

func (s *secondsValue) UnmarshalJSON(data []byte) error {
	var number json.Number
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		number = json.Number(text)
	} else {
		number = json.Number(string(data))
	}
	value, err := number.Int64()
	if err != nil {
		return err
	}
	*s = secondsValue(time.Duration(value) * time.Second)
	return nil
}

func (s secondsValue) Duration() time.Duration { return time.Duration(s) }

func tokenType(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "Bearer"
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
		return fmt.Errorf("invalid PINGCODE_WRITE_MODE %q: expected readonly, write, or destructive", rawMode)
	}
	return fmt.Errorf("PingCode %s request is blocked by PINGCODE_WRITE_MODE=%s", method, mode)
}
