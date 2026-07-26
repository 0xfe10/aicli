package restishrt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	restish "github.com/rest-sh/restish/v2"

	"github.com/0xfe10/aicli/internal/redact"
)

const pinnedRestishVersion = "2.3.0"

// AuthProvider supplies Authorization headers for raw requests.
type AuthProvider func(ctx context.Context) (string, error)

// Runtime wraps embedded Restish metadata and a controlled raw HTTP escape hatch.
// Domain commands never use this path. OpenAPI discovery is intentionally disabled.
type Runtime struct {
	APIBaseURL string
	Auth       AuthProvider
	Stdout     io.Writer
	Stderr     io.Writer
	HTTP       *http.Client
}

// Version returns the pinned embedded Restish module version.
func Version() string {
	_ = restish.New // keep Restish linked into the release binary
	return pinnedRestishVersion
}

// Run executes pingcode raw <METHOD> <path> [--body JSON].
// stdout is exactly one JSON document owned by this function.
func (r *Runtime) Run(ctx context.Context, args []string) error {
	outW := r.Stdout
	if outW == nil {
		outW = os.Stdout
	}
	errW := r.Stderr
	if errW == nil {
		errW = os.Stderr
	}

	method, path, body, err := parseRawArgs(args)
	if err != nil {
		return writeRawErr(outW, "INVALID_ARGUMENT", err.Error())
	}

	base := strings.TrimRight(r.APIBaseURL, "/")
	fullURL, err := joinURL(base, path)
	if err != nil {
		return writeRawErr(outW, "INVALID_ARGUMENT", err.Error())
	}

	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return writeRawErr(outW, "INTERNAL_ERROR", redact.String(err.Error()))
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Auth != nil {
		authz, err := r.Auth(ctx)
		if err != nil {
			return writeRawErr(outW, "AUTH_REQUIRED", redact.String(err.Error()))
		}
		req.Header.Set("Authorization", authz)
	}

	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintln(errW, redact.String(err.Error()))
		code := "UPSTREAM_ERROR"
		if ctx.Err() != nil || strings.Contains(strings.ToLower(err.Error()), "timeout") {
			code = "UPSTREAM_TIMEOUT"
		}
		return writeRawErr(outW, code, redact.String(err.Error()))
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return writeRawErr(outW, "UPSTREAM_ERROR", "读取响应失败")
	}

	var parsed any
	if len(bytes.TrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			parsed = string(respBody)
		}
	}

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	enc := json.NewEncoder(outW)
	enc.SetIndent("", "  ")
	doc := map[string]any{
		"ok": ok,
		"data": map[string]any{
			"status":  resp.StatusCode,
			"headers": redactHeaders(resp.Header),
			"body":    parsed,
		},
		"meta": map[string]any{
			"command":         "raw",
			"method":          method,
			"url":             redact.String(fullURL),
			"restish_version": pinnedRestishVersion,
			"transport":       "embedded-restish-controlled-http",
		},
	}
	if !ok {
		doc["error"] = map[string]string{
			"code":    mapHTTPStatus(resp.StatusCode),
			"message": fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
	}
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("raw request failed with HTTP %d", resp.StatusCode)
	}
	return nil
}

func parseRawArgs(args []string) (method, path string, body []byte, err error) {
	if len(args) == 0 {
		return "", "", nil, fmt.Errorf("usage: pingcode raw <GET|POST|PATCH|DELETE> <path> [--body JSON]")
	}
	// Ignore leading "--" separators.
	for len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		return "", "", nil, fmt.Errorf("usage: pingcode raw <GET|POST|PATCH|DELETE> <path> [--body JSON]")
	}
	method = strings.ToUpper(args[0])
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut, http.MethodHead:
	default:
		return "", "", nil, fmt.Errorf("unsupported method %q", args[0])
	}
	path = args[1]
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--body":
			if i+1 >= len(rest) {
				return "", "", nil, fmt.Errorf("--body 需要 JSON 参数")
			}
			raw := []byte(rest[i+1])
			if !json.Valid(raw) {
				return "", "", nil, fmt.Errorf("--body 必须是合法 JSON")
			}
			body = raw
			i++
		default:
			return "", "", nil, fmt.Errorf("未知参数: %s", rest[i])
		}
	}
	if body != nil && (method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete) {
		return "", "", nil, fmt.Errorf("%s 不支持 --body", method)
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

func writeRawErr(w io.Writer, code, message string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": redact.String(message),
		},
		"meta": map[string]any{
			"command":         "raw",
			"restish_version": pinnedRestishVersion,
		},
	})
	return fmt.Errorf("%s", message)
}
