package restishrt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	restish "github.com/rest-sh/restish/v2"

	"github.com/0xfe10/aicli/internal/redact"
)

const (
	pinnedRestishVersion = "2.3.0"
	maxResponseBodyBytes = 8 << 20
	maxRedirects         = 10
)

// AuthProvider supplies Authorization headers for raw requests.
type AuthProvider func(ctx context.Context) (string, error)

// Runtime provides a controlled raw HTTP escape hatch for pingcode raw.
// Domain commands never use this path. OpenAPI discovery is intentionally disabled.
// Restish remains linked for version/compliance inventory only; requests use net/http.
type Runtime struct {
	APIBaseURL string
	Auth       AuthProvider
	HTTP       *http.Client
}

// Result is returned to the command layer, which owns stdout encoding.
type Result struct {
	Data any
	Meta any
}

// Error carries a stable CLI error code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string     { return e.Message }
func (e *Error) ErrorCode() string { return e.Code }

// Version returns the pinned embedded Restish module version.
func Version() string {
	_ = restish.New // keep Restish linked into the release binary
	return pinnedRestishVersion
}

// Run executes pingcode raw <METHOD> <path> [--body JSON | --body-stdin].
// It never writes stdout or stderr; the command layer encodes exactly once.
// Prefer --body-stdin for payloads that may contain credentials.
func (r *Runtime) Run(ctx context.Context, args []string, stdin io.Reader) (Result, error) {
	method, path, body, err := parseRawArgs(args, stdin)
	if err != nil {
		return Result{}, rawError("INVALID_ARGUMENT", err.Error())
	}

	base := strings.TrimRight(r.APIBaseURL, "/")
	fullURL, err := joinURL(base, path)
	if err != nil {
		return Result{}, rawError("INVALID_ARGUMENT", err.Error())
	}

	client := r.httpClient()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return Result{}, rawError("INTERNAL_ERROR", err.Error())
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Auth != nil {
		authz, err := r.Auth(ctx)
		if err != nil {
			return Result{}, rawError("AUTH_REQUIRED", err.Error())
		}
		req.Header.Set("Authorization", authz)
	}

	resp, err := client.Do(req)
	if err != nil {
		code := "UPSTREAM_ERROR"
		msg := err.Error()
		lower := strings.ToLower(msg)
		switch {
		case ctx.Err() != nil || strings.Contains(lower, "timeout"):
			code = "UPSTREAM_TIMEOUT"
		case strings.Contains(lower, "cross-origin redirect") || strings.Contains(lower, "跨主机") || strings.Contains(lower, "跨端口"):
			code = "FORBIDDEN"
		}
		return Result{}, rawError(code, msg)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBodyBytes+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return Result{}, rawError("UPSTREAM_ERROR", "读取响应失败")
	}
	truncated := len(respBody) > maxResponseBodyBytes
	if truncated {
		respBody = respBody[:maxResponseBodyBytes]
	}

	var parsed any
	if len(bytes.TrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			parsed = redact.String(string(respBody))
		} else {
			parsed = redact.Value(parsed)
		}
	}

	data := map[string]any{
		"status":  resp.StatusCode,
		"headers": redactHeaders(resp.Header),
		"body":    parsed,
	}
	meta := map[string]any{
		"command":            "raw",
		"method":             method,
		"url":                redact.String(fullURL),
		"restish_version":    pinnedRestishVersion,
		"transport":          "controlled-net-http",
		"restish_role":       "linked-only",
		"response_truncated": truncated,
		"response_bytes_cap": maxResponseBodyBytes,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Keep response data for debugging; command layer still emits one JSON document.
		return Result{Data: data, Meta: meta}, rawError(mapHTTPStatus(resp.StatusCode), fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	return Result{Data: data, Meta: meta}, nil
}

func (r *Runtime) httpClient() *http.Client {
	base := r.HTTP
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	// Shallow copy so we never mutate a caller-owned client.
	client := *base
	client.CheckRedirect = secureCheckRedirect
	return &client
}

// secureCheckRedirect refuses redirects that change host or port.
// Go's default policy still forwards Authorization across ports on the same host.
func secureCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if !sameAuthority(origin, req.URL) {
		return fmt.Errorf("raw 拒绝跟随跨主机或跨端口重定向（%s -> %s）", authority(origin), authority(req.URL))
	}
	return nil
}

func sameAuthority(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return authority(a) == authority(b)
}

func authority(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return host + ":" + port
}

func parseRawArgs(args []string, stdin io.Reader) (method, path string, body []byte, err error) {
	usage := "usage: pingcode raw <GET|POST|PATCH|DELETE> <path> [--body JSON | --body-stdin]"
	if len(args) == 0 {
		return "", "", nil, fmt.Errorf("%s", usage)
	}
	for len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		return "", "", nil, fmt.Errorf("%s", usage)
	}
	method = strings.ToUpper(args[0])
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut, http.MethodHead:
	default:
		return "", "", nil, fmt.Errorf("unsupported method %q", args[0])
	}
	path = args[1]
	rest := args[2:]
	var bodyFromArg, bodyFromStdin bool
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--body":
			if bodyFromStdin {
				return "", "", nil, fmt.Errorf("--body 与 --body-stdin 不能同时使用")
			}
			if i+1 >= len(rest) {
				return "", "", nil, fmt.Errorf("--body 需要 JSON 参数")
			}
			raw := []byte(rest[i+1])
			if !json.Valid(raw) {
				return "", "", nil, fmt.Errorf("--body 必须是合法 JSON")
			}
			body = raw
			bodyFromArg = true
			i++
		case "--body-stdin":
			if bodyFromArg {
				return "", "", nil, fmt.Errorf("--body 与 --body-stdin 不能同时使用")
			}
			if stdin == nil {
				return "", "", nil, fmt.Errorf("--body-stdin 需要可读的 stdin")
			}
			raw, readErr := io.ReadAll(io.LimitReader(stdin, maxResponseBodyBytes))
			if readErr != nil {
				return "", "", nil, fmt.Errorf("读取 stdin 失败: %w", readErr)
			}
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 {
				return "", "", nil, fmt.Errorf("--body-stdin JSON 为空")
			}
			if !json.Valid(raw) {
				return "", "", nil, fmt.Errorf("--body-stdin 必须是合法 JSON")
			}
			body = raw
			bodyFromStdin = true
		default:
			return "", "", nil, fmt.Errorf("未知参数: %s", rest[i])
		}
	}
	if body != nil && (method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete) {
		return "", "", nil, fmt.Errorf("%s 不支持请求体", method)
	}
	return method, path, body, nil
}

func joinURL(base, path string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return "", fmt.Errorf("raw 只允许相对 API 路径，禁止覆盖主机")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		// httptest uses http://127.0.0.1 — already allowed via IP check above for tests.
		if u.Scheme == "http" && (strings.HasPrefix(u.Host, "127.0.0.1") || strings.HasPrefix(u.Host, "localhost")) {
			return u.String(), nil
		}
		if u.Scheme != "http" {
			return "", fmt.Errorf("API URL 必须是 HTTPS")
		}
	}
	return u.String(), nil
}

func redactHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vals := range h {
		if len(vals) == 0 {
			continue
		}
		out[k] = redact.HeaderValue(k, vals[0])
	}
	return out
}

func mapHTTPStatus(status int) string {
	switch status {
	case 401:
		return "AUTH_EXPIRED"
	case 403:
		return "FORBIDDEN"
	case 404:
		return "NOT_FOUND"
	case 429:
		return "RATE_LIMITED"
	default:
		if status >= 500 {
			return "UPSTREAM_ERROR"
		}
		return "UPSTREAM_ERROR"
	}
}

func rawError(code, message string) error {
	return &Error{Code: code, Message: redact.String(message)}
}
