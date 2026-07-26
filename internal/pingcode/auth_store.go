package pingcode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// StoredTokens is the on-disk user OAuth token payload.
type StoredTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	TokenType    string `json:"tokenType"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"` // unix milliseconds
	SavedAt      int64  `json:"savedAt"`
}

// AuthStore persists user tokens with 0600 permissions and atomic rename.
type AuthStore struct {
	path    string
	mu      sync.Mutex
	cache   *StoredTokens
	loaded  bool
	loadErr error
}

// NewAuthStore creates a token store for path.
func NewAuthStore(path string) *AuthStore {
	return &AuthStore{path: path}
}

// Path returns the token file path.
func (s *AuthStore) Path() string {
	return s.path
}

// Get returns cached or on-disk tokens.
// Missing file => (nil, nil) meaning not logged in.
// Insecure, wrong type, symlink, or invalid JSON => error.
func (s *AuthStore) Get() (*StoredTokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		s.loaded = true
		tokens, err := readTokenFile(s.path)
		if err != nil {
			s.cache = nil
			s.loadErr = err
			return nil, err
		}
		s.cache = tokens
		s.loadErr = nil
	}
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.cache == nil {
		return nil, nil
	}
	cp := *s.cache
	return &cp, nil
}

// HasToken reports whether a usable user access token is present.
func (s *AuthStore) HasToken() bool {
	t, err := s.Get()
	return err == nil && t != nil && t.AccessToken != ""
}

// Save atomically writes tokens with directory 0700 and file 0600.
func (s *AuthStore) Save(tokens StoredTokens) (*StoredTokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tokens.SavedAt == 0 {
		tokens.SavedAt = time.Now().UnixMilli()
	}
	if tokens.TokenType == "" {
		tokens.TokenType = "Bearer"
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, WrapError(CodeInternalError, "无法创建 token 存储目录", err)
	}
	_ = os.Chmod(dir, 0o700)

	tmp, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return nil, WrapError(CodeInternalError, "无法创建临时 token 文件", err)
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tokens); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return nil, WrapError(CodeInternalError, "无法序列化 token", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return nil, WrapError(CodeInternalError, "无法同步 token 文件", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, WrapError(CodeInternalError, "无法关闭临时 token 文件", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return nil, WrapError(CodeInternalError, "无法设置 token 文件权限", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return nil, WrapError(CodeInternalError, "无法原子写入 token 文件", err)
	}
	_ = os.Chmod(s.path, 0o600)

	cp := tokens
	s.cache = &cp
	s.loaded = true
	s.loadErr = nil
	out := tokens
	return &out, nil
}

// Update merges partial token fields and saves.
func (s *AuthStore) Update(partial StoredTokens) (*StoredTokens, error) {
	current, err := s.Get()
	if err != nil {
		return nil, err
	}
	merged := StoredTokens{
		AccessToken:  partial.AccessToken,
		RefreshToken: partial.RefreshToken,
		TokenType:    partial.TokenType,
		ExpiresAt:    partial.ExpiresAt,
		SavedAt:      time.Now().UnixMilli(),
	}
	if current != nil {
		if merged.AccessToken == "" {
			merged.AccessToken = current.AccessToken
		}
		if merged.RefreshToken == "" {
			merged.RefreshToken = current.RefreshToken
		}
		if merged.TokenType == "" {
			merged.TokenType = current.TokenType
		}
		if merged.ExpiresAt == 0 {
			merged.ExpiresAt = current.ExpiresAt
		}
	}
	if merged.TokenType == "" {
		merged.TokenType = "Bearer"
	}
	return s.Save(merged)
}

// Clear deletes the token file.
func (s *AuthStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
	s.loaded = true
	s.loadErr = nil
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return WrapError(CodeInternalError, "无法删除本地用户 Token", err)
	}
	return nil
}

// InspectTokenFile returns a redacted diagnostic for config check.
func InspectTokenFile(path string) map[string]any {
	out := map[string]any{
		"path":   path,
		"exists": false,
		"ok":     false,
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			out["status"] = "missing"
			out["ok"] = true // missing means not logged in, not a config corruption
			return out
		}
		out["status"] = "error"
		out["error"] = err.Error()
		return out
	}
	out["exists"] = true
	out["mode"] = fmt.Sprintf("%04o", info.Mode().Perm())
	if info.Mode()&os.ModeSymlink != 0 {
		out["status"] = "symlink_rejected"
		return out
	}
	if !info.Mode().IsRegular() {
		out["status"] = "not_regular_file"
		return out
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o177 != 0 {
		out["status"] = "permissions_too_open"
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		out["status"] = "unreadable"
		out["error"] = err.Error()
		return out
	}
	var parsed StoredTokens
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.AccessToken == "" {
		out["status"] = "invalid_json"
		return out
	}
	out["status"] = "ok"
	out["ok"] = true
	out["hasRefreshToken"] = parsed.RefreshToken != ""
	return out
}

func readTokenFile(path string) (*StoredTokens, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, WrapError(CodeInternalError, "无法读取 token 文件", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, NewError(CodeConfigMissing, "拒绝使用符号链接作为 token 文件")
	}
	if !info.Mode().IsRegular() {
		return nil, NewError(CodeConfigMissing, "token 路径必须是普通文件")
	}
	if runtime.GOOS != "windows" {
		perm := info.Mode().Perm()
		if perm&0o177 != 0 {
			return nil, NewError(CodeConfigMissing, fmt.Sprintf("token 文件权限过宽（%04o），要求不超过 0600", perm))
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, WrapError(CodeInternalError, "无法读取 token 文件", err)
	}
	var parsed StoredTokens
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, NewError(CodeConfigMissing, "token 文件 JSON 无效")
	}
	if parsed.AccessToken == "" {
		return nil, NewError(CodeConfigMissing, "token 文件缺少 accessToken")
	}
	if parsed.TokenType == "" {
		parsed.TokenType = "Bearer"
	}
	return &parsed, nil
}

// OAuthState is the pending authorization state.
type OAuthState struct {
	State     string `json:"state"`
	CreatedAt int64  `json:"createdAt"`
}

// OAuthStateTTL is the maximum age of a pending OAuth state.
const OAuthStateTTL = 10 * time.Minute

// nowFunc is injectable for OAuth TTL tests.
var nowFunc = time.Now

// SetNowFunc overrides the clock used for OAuth state TTL. Tests should restore with time.Now.
func SetNowFunc(fn func() time.Time) {
	if fn == nil {
		nowFunc = time.Now
		return
	}
	nowFunc = fn
}

// SaveOAuthState atomically stores a pending OAuth state.
func SaveOAuthState(path, state string) error {
	payload := OAuthState{State: state, CreatedAt: nowFunc().UnixMilli()}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return WrapError(CodeInternalError, "无法创建 OAuth state 目录", err)
	}
	_ = os.Chmod(dir, 0o700)
	tmp, err := os.CreateTemp(dir, ".oauth-state-*.tmp")
	if err != nil {
		return WrapError(CodeInternalError, "无法创建临时 OAuth state 文件", err)
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o600)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Chmod(path, 0o600)
}

// LoadOAuthState reads pending OAuth state and enforces TTL.
func LoadOAuthState(path string) (*OAuthState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewError(CodeAuthRequired, "没有待完成的 OAuth 登录；请先运行 pingcode auth login")
		}
		return nil, WrapError(CodeInternalError, "无法读取 OAuth state", err)
	}
	var state OAuthState
	if err := json.Unmarshal(data, &state); err != nil || state.State == "" || state.CreatedAt <= 0 {
		_ = ClearOAuthState(path)
		return nil, NewError(CodeAuthRequired, "OAuth state 无效；请重新运行 pingcode auth login")
	}
	age := nowFunc().Sub(time.UnixMilli(state.CreatedAt))
	if age < 0 || age > OAuthStateTTL {
		_ = ClearOAuthState(path)
		return nil, NewError(CodeAuthRequired, "OAuth state 已过期；请重新运行 pingcode auth login")
	}
	return &state, nil
}

// ClearOAuthState removes pending OAuth state.
func ClearOAuthState(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return WrapError(CodeInternalError, "无法清除 OAuth state", err)
	}
	return nil
}
