package lanhurt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	baseURL = "https://lanhuapp.com"
	cdnURL  = "https://axure-file.lanhuapp.com"
)

type Client struct {
	Cookie    string
	DDSCookie string
	HTTP      *http.Client
}

type Reference struct {
	TeamID     string
	ProjectID  string
	DocumentID string
	PageID     string
}

type Page struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Path     string `json:"path"`
	Level    int    `json:"level"`
}

type Document struct {
	ID        string `json:"document_id"`
	Name      string `json:"document_name"`
	VersionID string `json:"version_id"`
	Pages     []Page `json:"pages"`
	mapping   map[string]any
}

func ParseReference(raw string) (Reference, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Reference{}, fmt.Errorf("parse Lanhu URL: %w", err)
	}
	q := u.Query()
	if i := strings.IndexByte(u.Fragment, '?'); i >= 0 {
		fragment, err := url.ParseQuery(u.Fragment[i+1:])
		if err != nil {
			return Reference{}, fmt.Errorf("parse Lanhu fragment: %w", err)
		}
		for key, values := range fragment {
			if q.Get(key) == "" && len(values) > 0 {
				q.Set(key, values[0])
			}
		}
	}
	ref := Reference{TeamID: q.Get("tid"), ProjectID: q.Get("pid"), DocumentID: q.Get("docId"), PageID: first(q.Get("pageId"), q.Get("page_id"))}
	if ref.ProjectID == "" {
		ref.ProjectID = q.Get("project_id")
	}
	if ref.DocumentID == "" {
		ref.DocumentID = q.Get("image_id")
	}
	if ref.ProjectID == "" || ref.DocumentID == "" {
		return Reference{}, errors.New("Lanhu URL must contain pid/project_id and docId/image_id")
	}
	return ref, nil
}

func (c *Client) get(ctx context.Context, raw string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	host := req.URL.Hostname()
	if req.URL.Scheme != "https" || !allowedHost(host) {
		return fmt.Errorf("refusing unsupported Lanhu origin %q", req.URL.Host)
	}
	c.applyHeaders(req)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many Lanhu redirects")
		}
		if next.URL.Scheme != "https" || !allowedHost(next.URL.Hostname()) {
			return fmt.Errorf("refusing redirect to %q", next.URL.Host)
		}
		c.applyHeaders(next)
		return nil
	}
	resp, err := copy.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", req.URL.Host+req.URL.Path, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 64<<20 {
		return errors.New("Lanhu resource exceeds 64 MiB")
	}
	if writer, ok := target.(io.Writer); ok {
		_, err = writer.Write(data)
		return err
	}
	return json.Unmarshal(data, target)
}

func (c *Client) applyHeaders(req *http.Request) {
	req.Header.Del("Cookie")
	req.Header.Del("Authorization")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://lanhuapp.com/web/")
	req.Header.Set("request-from", "web")
	switch req.URL.Hostname() {
	case "lanhuapp.com":
		req.Header.Set("Cookie", c.Cookie)
	case "dds.lanhuapp.com":
		req.Header.Set("Cookie", first(c.DDSCookie, c.Cookie))
		req.Header.Set("Referer", "https://dds.lanhuapp.com/")
		req.Header.Set("Authorization", "Basic dW5kZWZpbmVkOg==")
	}
}

func allowedHost(host string) bool {
	return host == "lanhuapp.com" || strings.HasSuffix(host, ".lanhuapp.com") ||
		host == "lanhu.oss-cn-beijing.aliyuncs.com" || host == "lanhu-dds-backend.oss-cn-beijing.aliyuncs.com"
}

func (c *Client) LoadDocument(ctx context.Context, raw string) (Document, error) {
	ref, err := ParseReference(raw)
	if err != nil {
		return Document{}, err
	}
	endpoint, _ := url.Parse(baseURL + "/api/project/image")
	q := endpoint.Query()
	q.Set("pid", ref.ProjectID)
	q.Set("image_id", ref.DocumentID)
	endpoint.RawQuery = q.Encode()
	var response struct {
		Code   any    `json:"code"`
		Msg    string `json:"msg"`
		Result struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Versions []struct {
				ID      string `json:"id"`
				JSONURL string `json:"json_url"`
			} `json:"versions"`
		} `json:"result"`
		Data json.RawMessage `json:"data"`
	}
	if err := c.get(ctx, endpoint.String(), &response); err != nil {
		return Document{}, err
	}
	if fmt.Sprint(response.Code) == "10009" && ref.TeamID != "" {
		current, err := c.currentAxureDocument(ctx, ref.TeamID, ref.ProjectID, ref.PageID)
		if err != nil {
			return Document{}, err
		}
		q.Set("image_id", current)
		endpoint.RawQuery = q.Encode()
		response = struct {
			Code   any    `json:"code"`
			Msg    string `json:"msg"`
			Result struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Versions []struct {
					ID      string `json:"id"`
					JSONURL string `json:"json_url"`
				} `json:"versions"`
			} `json:"result"`
			Data json.RawMessage `json:"data"`
		}{}
		if err := c.get(ctx, endpoint.String(), &response); err != nil {
			return Document{}, err
		}
	}
	if fmt.Sprint(response.Code) != "0" && fmt.Sprint(response.Code) != "00000" {
		return Document{}, fmt.Errorf("Lanhu API rejected document: code=%v %s", response.Code, response.Msg)
	}
	if response.Result.ID == "" && len(response.Data) > 0 {
		if err := json.Unmarshal(response.Data, &response.Result); err != nil {
			return Document{}, err
		}
	}
	if len(response.Result.Versions) == 0 || response.Result.Versions[0].JSONURL == "" {
		return Document{}, errors.New("Lanhu document has no downloadable Axure version")
	}
	var mapping map[string]any
	if err := c.get(ctx, response.Result.Versions[0].JSONURL, &mapping); err != nil {
		return Document{}, fmt.Errorf("download Axure mapping: %w", err)
	}
	doc := Document{ID: response.Result.ID, Name: response.Result.Name, VersionID: response.Result.Versions[0].ID, mapping: mapping}
	if sitemap, ok := mapping["sitemap"].(map[string]any); ok {
		doc.Pages = appendPages(nil, sitemap["rootNodes"], "", 0)
	}
	return doc, nil
}

func (c *Client) currentAxureDocument(ctx context.Context, teamID, projectID, pageID string) (string, error) {
	endpoint, _ := url.Parse(baseURL + "/api/project/product_documents")
	q := endpoint.Query()
	q.Set("team_id", teamID)
	q.Set("project_id", projectID)
	endpoint.RawQuery = q.Encode()
	var envelope map[string]any
	if err := c.get(ctx, endpoint.String(), &envelope); err != nil {
		return "", err
	}
	result, err := apiResult(envelope)
	if err != nil {
		return "", err
	}
	resources, _ := result["resources"].([]any)
	ids := make([]string, 0)
	for _, item := range resources {
		resource, _ := item.(map[string]any)
		if text(resource, "type") == "axure" && text(resource, "id") != "" {
			ids = append(ids, text(resource, "id"))
		}
	}
	if len(ids) != 1 {
		if pageID != "" {
			for _, id := range ids {
				imageURL := baseURL + "/api/project/image?pid=" + url.QueryEscape(projectID) + "&image_id=" + url.QueryEscape(id)
				var imageEnvelope map[string]any
				if c.get(ctx, imageURL, &imageEnvelope) != nil {
					continue
				}
				imageResult, err := apiResult(imageEnvelope)
				if err != nil {
					continue
				}
				versions, _ := imageResult["versions"].([]any)
				if len(versions) == 0 {
					continue
				}
				version, _ := versions[0].(map[string]any)
				var mapping map[string]any
				if jsonURL := text(version, "json_url"); jsonURL != "" && c.get(ctx, jsonURL, &mapping) == nil && sitemapContains(mapping, pageID) {
					return id, nil
				}
			}
		}
		return "", fmt.Errorf("stale Axure docId resolves to %d candidates; use a current document URL", len(ids))
	}
	return ids[0], nil
}

func sitemapContains(mapping map[string]any, pageID string) bool {
	sitemap, _ := mapping["sitemap"].(map[string]any)
	stack, _ := sitemap["rootNodes"].([]any)
	for len(stack) > 0 {
		last := len(stack) - 1
		item := stack[last]
		stack = stack[:last]
		node, _ := item.(map[string]any)
		if text(node, "id") == pageID {
			return true
		}
		children, _ := node["children"].([]any)
		stack = append(stack, children...)
	}
	return false
}

func appendPages(out []Page, value any, parent string, level int) []Page {
	nodes, _ := value.([]any)
	for _, item := range nodes {
		node, _ := item.(map[string]any)
		name, _ := node["pageName"].(string)
		filename, _ := node["url"].(string)
		path := strings.Trim(strings.Join([]string{parent, name}, "/"), "/")
		if name != "" && filename != "" {
			page := Page{Name: name, Filename: filename, Path: path, Level: level}
			page.ID, _ = node["id"].(string)
			page.Type, _ = node["type"].(string)
			out = append(out, page)
		}
		out = appendPages(out, node["children"], path, level+1)
	}
	return out
}

func (c *Client) Download(ctx context.Context, doc Document, output string) error {
	pages, _ := doc.mapping["pages"].(map[string]any)
	for filename, raw := range pages {
		if err := safeRelative(filename); err != nil {
			return err
		}
		page, _ := raw.(map[string]any)
		html, _ := page["html"].(map[string]any)
		htmlPath, err := safeJoin(output, filename)
		if err != nil {
			return err
		}
		if err := c.downloadSigned(ctx, html, htmlPath); err != nil {
			return err
		}
		if err := patchHTML(htmlPath); err != nil {
			return err
		}
		if md5, _ := page["mapping_md5"].(string); md5 != "" {
			var assets map[string]any
			mappingURL, err := signedURL(md5)
			if err != nil {
				return err
			}
			if err := c.get(ctx, mappingURL, &assets); err != nil {
				return err
			}
			for _, group := range []string{"styles", "scripts", "images"} {
				entries, _ := assets[group].(map[string]any)
				for name, value := range entries {
					if err := safeRelative(name); err != nil {
						return err
					}
					entry, _ := value.(map[string]any)
					path, err := safeJoin(output, name)
					if err != nil {
						return err
					}
					if err := c.downloadSigned(ctx, entry, path); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func patchHTML(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	html := string(data)
	html = strings.ReplaceAll(html, "data-src=", "src=")
	for _, hidden := range []string{"display: none;", "display:none;", "opacity: 0;", "opacity:0;"} {
		html = strings.ReplaceAll(html, hidden, "")
	}
	shim := `<script>function lanhu_Axure_Mapping_Data(data){window.__lanhuAxurePageData=data;return data}</script>`
	if strings.Contains(html, "</head>") {
		html = strings.Replace(html, "</head>", shim+"</head>", 1)
	}
	return os.WriteFile(path, []byte(html), 0o644)
}

func (c *Client) downloadSigned(ctx context.Context, entry map[string]any, path string) error {
	sign, _ := entry["sign_md5"].(string)
	if sign == "" {
		return nil
	}
	rawURL, err := signedURL(sign)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.get(ctx, rawURL, f)
}

func signedURL(value string) (string, error) {
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		if parsed.Scheme != "https" || !allowedHost(parsed.Hostname()) {
			return "", fmt.Errorf("unsupported Axure resource origin %q", parsed.Host)
		}
		return parsed.String(), nil
	}
	value = strings.TrimPrefix(value, "/")
	if err := safeRelative(value); err != nil {
		return "", fmt.Errorf("unsafe Axure CDN key: %w", err)
	}
	return cdnURL + "/" + value, nil
}

func safeRelative(name string) error {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe Axure resource path %q", name)
	}
	return nil
}

func safeJoin(root, name string) (string, error) {
	if err := safeRelative(name); err != nil {
		return "", err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(root); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return "", fmt.Errorf("Axure output root must be a directory, not a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	current := root
	parts := strings.Split(filepath.Clean(filepath.FromSlash(name)), string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return "", fmt.Errorf("Axure output path contains a symlink or non-directory: %s", current)
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	if info, err := os.Lstat(target); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return "", fmt.Errorf("Axure output file must not be a symlink or special file: %s", target)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}

func Render(ctx context.Context, directory, page, output string) error {
	if err := safeRelative(page); err != nil {
		return err
	}
	chromium := os.Getenv("LANHU_CHROMIUM")
	if chromium == "" {
		for _, candidate := range []string{"chromium", "chromium-browser", "google-chrome"} {
			if found, err := exec.LookPath(candidate); err == nil {
				chromium = found
				break
			}
		}
	}
	if chromium == "" {
		return errors.New("Chromium not found; install it or set LANHU_CHROMIUM")
	}
	absPage, err := filepath.Abs(filepath.Join(directory, filepath.FromSlash(page)))
	if err != nil {
		return err
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absOutput), 0o755); err != nil {
		return err
	}
	profile, err := os.MkdirTemp("", "lanhu-chromium-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	arguments := []string{"--headless", "--disable-gpu", "--allow-file-access-from-files", "--user-data-dir=" + profile, "--window-size=1440,1200", "--screenshot=" + absOutput, "file://" + absPage}
	if os.Getenv("LANHU_CHROMIUM_NO_SANDBOX") == "1" {
		arguments = append([]string{"--no-sandbox"}, arguments...)
	}
	renderContext, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(renderContext, chromium, arguments...)
	if data, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Chromium render failed: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return nil
}
