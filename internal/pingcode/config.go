package pingcode

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds PingCode CLI configuration loaded from the environment.
type Config struct {
	BaseURL             string
	APIBaseURL          string
	AccessToken         string
	ClientID            string
	ClientSecret        string
	AuthScheme          string
	OAuthAuthorizeURL   string
	OAuthRedirectURI    string
	AuthTokenPath       string
	AuthStatePath       string
	ProjectIdentifier   string
	ProjectID           string
	DefaultAssigneeName string
	BugTypeID           string
	RequirementTypeID   string
	Readonly            bool
	TimeoutMS           int
}

// LoadConfig reads configuration from process environment variables.
func LoadConfig() (Config, error) {
	baseURL := trimTrailingSlash(envOr("PINGCODE_BASE_URL", "https://your-domain.pingcode.com"))
	apiBaseURL := trimTrailingSlash(envOr("PINGCODE_API_BASE_URL", "https://open.pingcode.com"))

	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, WrapError(CodeConfigMissing, "无法解析用户主目录以确定配置路径", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	authTokenPath := envOr("PINGCODE_AUTH_TOKEN_PATH", filepath.Join(configHome, "aicli", "pingcode", "auth.json"))

	cfg := Config{
		BaseURL:             baseURL,
		APIBaseURL:          apiBaseURL,
		AccessToken:         optionalEnv("PINGCODE_ACCESS_TOKEN"),
		ClientID:            optionalEnv("PINGCODE_CLIENT_ID"),
		ClientSecret:        optionalEnv("PINGCODE_CLIENT_SECRET"),
		AuthScheme:          envOr("PINGCODE_AUTH_SCHEME", "Bearer"),
		OAuthAuthorizeURL:   envOr("PINGCODE_OAUTH_AUTHORIZE_URL", baseURL+"/oauth2/authorize"),
		OAuthRedirectURI:    optionalEnv("PINGCODE_OAUTH_REDIRECT_URI"),
		AuthTokenPath:       authTokenPath,
		AuthStatePath:       filepath.Join(filepath.Dir(authTokenPath), "oauth-state.json"),
		ProjectIdentifier:   optionalEnv("PINGCODE_PROJECT_IDENTIFIER"),
		ProjectID:           optionalEnv("PINGCODE_PROJECT_ID"),
		DefaultAssigneeName: optionalEnv("PINGCODE_DEFAULT_ASSIGNEE_NAME"),
		BugTypeID:           envOr("PINGCODE_BUG_TYPE_ID", "bug"),
		RequirementTypeID:   optionalEnv("PINGCODE_REQUIREMENT_TYPE_ID"),
		Readonly:            readBool(os.Getenv("PINGCODE_READONLY"), false),
		TimeoutMS:           readInt(os.Getenv("PINGCODE_TIMEOUT_MS"), 15000),
	}

	if err := validateAPIBaseURL(cfg.APIBaseURL); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateAPIBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return NewError(CodeConfigMissing, "PINGCODE_API_BASE_URL 无效")
	}
	host := strings.ToLower(u.Hostname())
	isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !isLocal {
		return NewError(CodeConfigMissing, "PINGCODE_API_BASE_URL 必须使用 HTTPS（localhost 除外）")
	}
	return nil
}

// AssertWritable rejects writes when readonly mode is enabled.
func AssertWritable(cfg Config) error {
	if cfg.Readonly {
		return NewError(CodeReadonly, "PINGCODE_READONLY=true，写操作已被禁用。")
	}
	return nil
}

// CheckConfig returns a redacted configuration diagnostic report.
func CheckConfig(cfg Config) map[string]any {
	tokenInspect := InspectTokenFile(cfg.AuthTokenPath)
	authSources := []string{}
	if ok, _ := tokenInspect["ok"].(bool); ok {
		if status, _ := tokenInspect["status"].(string); status == "ok" {
			authSources = append(authSources, "user-token-file")
		}
	}
	if cfg.AccessToken != "" {
		authSources = append(authSources, "env-token")
	}
	if cfg.ClientID != "" && cfg.ClientSecret != "" {
		authSources = append(authSources, "client-credentials")
	}
	if len(authSources) == 0 {
		authSources = append(authSources, "missing")
	}

	tokenDir := filepath.Dir(cfg.AuthTokenPath)
	dirMode := ""
	if info, err := os.Stat(tokenDir); err == nil {
		dirMode = fmt.Sprintf("%04o", info.Mode().Perm())
	}

	issues := []string{}
	if cfg.BaseURL == "" || strings.Contains(cfg.BaseURL, "your-domain") {
		issues = append(issues, "PINGCODE_BASE_URL 未配置为真实租户地址")
	}
	if status, _ := tokenInspect["status"].(string); status != "" && status != "missing" && status != "ok" {
		issues = append(issues, "token 文件不可用: "+status)
	}
	if len(authSources) == 1 && authSources[0] == "missing" {
		issues = append(issues, "缺少可用认证来源")
	}
	if cfg.ProjectIdentifier == "" && cfg.ProjectID == "" {
		issues = append(issues, "未设置默认项目 PINGCODE_PROJECT_IDENTIFIER/PINGCODE_PROJECT_ID")
	}

	fileMode, _ := tokenInspect["mode"].(string)
	return map[string]any{
		"tenantURL":           cfg.BaseURL,
		"apiURL":              cfg.APIBaseURL,
		"authSources":         authSources,
		"authTokenPath":       cfg.AuthTokenPath,
		"authTokenDirMode":    dirMode,
		"authTokenFileMode":   fileMode,
		"authTokenFile":       tokenInspect,
		"projectIdentifier":   cfg.ProjectIdentifier,
		"projectIDSet":        cfg.ProjectID != "",
		"defaultAssigneeName": cfg.DefaultAssigneeName != "",
		"readonly":            cfg.Readonly,
		"timeoutMs":           cfg.TimeoutMS,
		"ok":                  len(issues) == 0,
		"issues":              issues,
	}
}

func envOr(key, fallback string) string {
	if v := optionalEnv(key); v != "" {
		return v
	}
	return fallback
}

func optionalEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func trimTrailingSlash(v string) string {
	return strings.TrimRight(v, "/")
}

func readBool(raw string, fallback bool) bool {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return fallback
	}
}

func readInt(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return n
}
