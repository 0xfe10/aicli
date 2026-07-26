package pingcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"time"
)

// AuthService orchestrates user OAuth login/status/logout.
type AuthService struct {
	cfg    Config
	store  *AuthStore
	client *Client
}

// NewAuthService constructs an auth service.
func NewAuthService(cfg Config, store *AuthStore, client *Client) *AuthService {
	return &AuthService{cfg: cfg, store: store, client: client}
}

// AuthStatus is a redacted authentication status report.
type AuthStatus struct {
	Mode             string `json:"mode"`
	HasUserToken     bool   `json:"hasUserToken"`
	ExpiresInSeconds *int64 `json:"expiresInSeconds,omitempty"`
	User             *User  `json:"user,omitempty"`
	Note             string `json:"note,omitempty"`
}

// BuildAuthorizeURL creates an OAuth authorize URL and persists a random state.
func (a *AuthService) BuildAuthorizeURL() (map[string]any, error) {
	if a.cfg.ClientID == "" {
		return nil, NewError(CodeConfigMissing, "缺少 PINGCODE_CLIENT_ID，无法发起浏览器授权。请配置 PINGCODE_CLIENT_ID / PINGCODE_CLIENT_SECRET，并在 PingCode 后台凭据管理中设置 redirect_uri。")
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	if err := SaveOAuthState(a.cfg.AuthStatePath, state); err != nil {
		return nil, err
	}
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {a.cfg.ClientID},
		"state":         {state},
	}
	if a.cfg.OAuthRedirectURI != "" {
		params.Set("redirect_uri", a.cfg.OAuthRedirectURI)
	}
	authorizeURL := a.cfg.OAuthAuthorizeURL + "?" + params.Encode()
	return map[string]any{
		"url":   authorizeURL,
		"state": state,
		"next":  "在浏览器打开 url，登录后把完整回调 URL 通过 stdin 传给 pingcode auth complete --callback-url-stdin",
	}, nil
}

// CompleteFromCallbackURL validates state, exchanges code, and stores tokens.
// The callback URL must arrive via stdin, never argv.
func (a *AuthService) CompleteFromCallbackURL(ctx context.Context, callbackURL string) (map[string]any, error) {
	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		return nil, NewError(CodeInvalidInput, "回调 URL 为空")
	}
	u, err := url.Parse(callbackURL)
	if err != nil {
		return nil, NewError(CodeInvalidInput, "回调 URL 无效")
	}
	code := u.Query().Get("code")
	state := u.Query().Get("state")
	if code == "" {
		return nil, NewError(CodeInvalidInput, "回调 URL 缺少 code")
	}
	if state == "" {
		return nil, NewError(CodeInvalidInput, "回调 URL 缺少 state")
	}
	saved, err := LoadOAuthState(a.cfg.AuthStatePath)
	if err != nil {
		return nil, err
	}
	if saved.State != state {
		return nil, NewError(CodeAuthRequired, "OAuth state 不匹配；请重新运行 pingcode auth login")
	}
	token, err := a.client.ExchangeAuthorizationCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if _, err := a.store.Save(StoredTokens{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    firstNonEmpty(token.TokenType, "Bearer"),
		ExpiresAt:    NormalizeExpiresAt(token.ExpiresIn, time.Now()),
		SavedAt:      time.Now().UnixMilli(),
	}); err != nil {
		return nil, err
	}
	_ = ClearOAuthState(a.cfg.AuthStatePath)
	user, err := a.client.GetCurrentUser(ctx)
	if err != nil {
		return map[string]any{"ok": true, "user": nil}, nil
	}
	return map[string]any{"ok": true, "user": user}, nil
}

// Status returns a redacted auth status.
func (a *AuthService) Status(ctx context.Context) (AuthStatus, error) {
	if stored := a.store.Get(); stored != nil && stored.AccessToken != "" {
		var expires *int64
		if stored.ExpiresAt > 0 {
			secs := (stored.ExpiresAt - time.Now().UnixMilli()) / 1000
			if secs < 0 {
				secs = 0
			}
			expires = &secs
		}
		status := AuthStatus{Mode: "user", HasUserToken: true, ExpiresInSeconds: expires}
		user, err := a.client.GetCurrentUser(ctx)
		if err == nil {
			status.User = &user
		}
		return status, nil
	}
	if a.cfg.AccessToken != "" {
		return AuthStatus{Mode: "env-token", HasUserToken: false}, nil
	}
	if a.cfg.ClientID != "" && a.cfg.ClientSecret != "" {
		return AuthStatus{
			Mode:         "application",
			HasUserToken: false,
			Note:         "未授权，将使用 client_credentials/默认负责人。",
		}, nil
	}
	return AuthStatus{Mode: "missing", HasUserToken: false, Note: "未配置任何认证来源。"}, nil
}

// Logout clears the local user token file only.
func (a *AuthService) Logout() (map[string]any, error) {
	if err := a.store.Clear(); err != nil {
		return nil, err
	}
	_ = ClearOAuthState(a.cfg.AuthStatePath)
	return map[string]any{"ok": true, "cleared": true}, nil
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", WrapError(CodeInternalError, "无法生成 OAuth state", err)
	}
	return hex.EncodeToString(buf), nil
}
