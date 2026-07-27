package pingcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// MCP-validated API path prefixes. Prefer /v1/project over /v1/pjm until
// authenticated response parity is proven for a tenant.
const (
	pathAuthToken          = "/v1/auth/token"
	pathMyself             = "/v1/myself"
	pathDirectoryTeam      = "/v1/directory/team"
	pathDirectoryUsers     = "/v1/directory/users"
	pathProjects           = "/v1/project/projects"
	pathWorkItemTypes      = "/v1/project/work_item/types"
	pathWorkItemStates     = "/v1/project/work_item/states"
	pathWorkItemPriorities = "/v1/project/work_item/priorities"
	pathWorkItemStatePlans = "/v1/project/work_item_state_plans"
	pathWorkItems          = "/v1/project/work_items"
	pathComments           = "/v1/comments"
)

const (
	defaultUserTokenTTLSeconds = 30 * 24 * 3600
	maxReadRetriesOn429        = 2
)

// Client is the typed PingCode Open API HTTP client.
type Client struct {
	cfg             Config
	store           *AuthStore
	http            *http.Client
	mu              sync.Mutex
	cached          string
	cachedExpiresAt time.Time

	// test hooks
	now func() time.Time
}

// NewClient constructs a PingCode API client.
func NewClient(cfg Config, store *AuthStore) *Client {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		cfg:   cfg,
		store: store,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				prev := via[len(via)-1]
				if prev.Header.Get("Authorization") != "" && req.URL.Host != prev.URL.Host {
					return NewError(CodeUpstreamError, "拒绝带认证请求跨主机跟随重定向")
				}
				if len(via) >= 5 {
					return NewError(CodeUpstreamError, "重定向次数过多")
				}
				return nil
			},
		},
		now: time.Now,
	}
}

// AuthorizationHeader returns the current Authorization value without logging it.
func (c *Client) AuthorizationHeader(ctx context.Context) (string, error) {
	return c.getAuthorization(ctx)
}

func (c *Client) ListProjects(ctx context.Context, identifier string, pageIndex, pageSize int) (PageResponse[Project], error) {
	q := url.Values{}
	if identifier != "" {
		q.Set("identifier", identifier)
	}
	q.Set("include_archived", "false")
	q.Set("include_deleted", "false")
	if pageIndex >= 0 {
		q.Set("page_index", strconv.Itoa(pageIndex))
	}
	if pageSize > 0 {
		q.Set("page_size", strconv.Itoa(pageSize))
	}
	var out PageResponse[Project]
	err := c.request(ctx, http.MethodGet, pathProjects, q, nil, true, &out)
	return out, err
}

func (c *Client) ResolveProject(ctx context.Context, projectIdentifier, projectID string) (Project, error) {
	if projectID != "" {
		// Exact ID path: still list/filter when identifier also provided for consistency checks,
		// but ID is authoritative and must not fall back to another project.
		return Project{
			ID:         projectID,
			Identifier: firstNonEmpty(projectIdentifier, c.cfg.ProjectIdentifier),
			Name:       firstNonEmpty(projectIdentifier, projectID),
		}, nil
	}
	identifier := firstNonEmpty(projectIdentifier, c.cfg.ProjectIdentifier)
	if identifier == "" {
		return Project{}, NewError(CodeConfigMissing, "未配置 PingCode 项目标识；请传入 --identifier/--project-id，或设置 PINGCODE_PROJECT_IDENTIFIER。")
	}
	page, err := c.ListProjects(ctx, identifier, 0, 30)
	if err != nil {
		return Project{}, err
	}
	var exact []Project
	for _, item := range page.Values {
		if item.Identifier == identifier || item.ID == identifier {
			exact = append(exact, item)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return Project{}, NewError(CodeAmbiguousName, fmt.Sprintf("项目标识存在歧义：%s", identifier))
	}
	candidates := make([]string, 0, len(page.Values))
	for _, item := range page.Values {
		candidates = append(candidates, firstNonEmpty(item.Identifier, item.Name, item.ID))
		if len(candidates) >= 5 {
			break
		}
	}
	msg := fmt.Sprintf("未找到 PingCode 项目：%s", identifier)
	if len(candidates) > 0 {
		msg += "；相近候选项：" + strings.Join(candidates, ", ")
	}
	return Project{}, NewError(CodeNotFound, msg)
}

func (c *Client) GetCurrentUser(ctx context.Context) (User, error) {
	var out User
	err := c.request(ctx, http.MethodGet, pathMyself, nil, nil, true, &out)
	return out, err
}

func (c *Client) GetCurrentTeam(ctx context.Context) (Team, error) {
	var out Team
	err := c.request(ctx, http.MethodGet, pathDirectoryTeam, nil, nil, true, &out)
	return out, err
}

func (c *Client) ListEnterpriseUsers(ctx context.Context, keywords string, pageSize int) (PageResponse[User], error) {
	q := url.Values{}
	if keywords != "" {
		q.Set("keywords", keywords)
	}
	if pageSize > 0 {
		q.Set("page_size", strconv.Itoa(pageSize))
	}
	var out PageResponse[User]
	err := c.request(ctx, http.MethodGet, pathDirectoryUsers, q, nil, true, &out)
	return out, err
}

func (c *Client) ListWorkItemTypes(ctx context.Context, projectID string, pageIndex, pageSize int) (PageResponse[WorkItemType], error) {
	q := url.Values{"project_id": {projectID}}
	setPage(q, pageIndex, pageSize)
	var out PageResponse[WorkItemType]
	err := c.request(ctx, http.MethodGet, pathWorkItemTypes, q, nil, true, &out)
	return out, err
}

func (c *Client) ListWorkItemStates(ctx context.Context, projectID, typeID string, pageIndex, pageSize int) (PageResponse[WorkItemState], error) {
	q := url.Values{"project_id": {projectID}, "work_item_type_id": {typeID}}
	setPage(q, pageIndex, pageSize)
	var out PageResponse[WorkItemState]
	err := c.request(ctx, http.MethodGet, pathWorkItemStates, q, nil, true, &out)
	return out, err
}

func (c *Client) ListWorkItemPriorities(ctx context.Context, projectID string, pageIndex, pageSize int) (PageResponse[WorkItemPriority], error) {
	q := url.Values{"project_id": {projectID}}
	setPage(q, pageIndex, pageSize)
	var out PageResponse[WorkItemPriority]
	err := c.request(ctx, http.MethodGet, pathWorkItemPriorities, q, nil, true, &out)
	return out, err
}

func (c *Client) ListProjectMembers(ctx context.Context, projectID string, pageIndex, pageSize int) (PageResponse[ProjectMember], error) {
	q := url.Values{}
	setPage(q, pageIndex, pageSize)
	path := fmt.Sprintf("%s/%s/members", pathProjects, url.PathEscape(projectID))
	var out PageResponse[ProjectMember]
	err := c.request(ctx, http.MethodGet, path, q, nil, true, &out)
	return out, err
}

func (c *Client) ListWorkItemStatePlans(ctx context.Context, projectID string, pageIndex, pageSize int) (PageResponse[WorkItemStatePlan], error) {
	q := url.Values{"project_id": {projectID}}
	setPage(q, pageIndex, pageSize)
	var out PageResponse[WorkItemStatePlan]
	err := c.request(ctx, http.MethodGet, pathWorkItemStatePlans, q, nil, true, &out)
	return out, err
}

func (c *Client) ListWorkItemStateFlows(ctx context.Context, statePlanID, fromStateID string, pageIndex, pageSize int) (PageResponse[WorkItemStateFlow], error) {
	q := url.Values{"from_state_id": {fromStateID}}
	setPage(q, pageIndex, pageSize)
	path := fmt.Sprintf("%s/%s/work_item_state_flows", pathWorkItemStatePlans, url.PathEscape(statePlanID))
	var out PageResponse[WorkItemStateFlow]
	err := c.request(ctx, http.MethodGet, path, q, nil, true, &out)
	return out, err
}

func (c *Client) ListWorkItems(ctx context.Context, query WorkItemListQuery) (PageResponse[WorkItem], error) {
	q := url.Values{}
	setIf(q, "identifier", query.Identifier)
	setIf(q, "project_ids", query.ProjectIDs)
	setIf(q, "type_ids", query.TypeIDs)
	setIf(q, "state_ids", query.StateIDs)
	setIf(q, "assignee_ids", query.AssigneeIDs)
	setIf(q, "priority_ids", query.PriorityIDs)
	setIf(q, "parent_ids", query.ParentIDs)
	setIf(q, "keywords", query.Keywords)
	setIf(q, "updated_between", query.UpdatedBetween)
	if query.IncludeDeleted != nil {
		q.Set("include_deleted", strconv.FormatBool(*query.IncludeDeleted))
	}
	if query.IncludeArchived != nil {
		q.Set("include_archived", strconv.FormatBool(*query.IncludeArchived))
	}
	if query.IncludePublicImageToken != nil {
		q.Set("include_public_image_token", strconv.FormatBool(*query.IncludePublicImageToken))
	}
	pageIndex := 0
	pageSize := 30
	if query.PageIndex != nil {
		pageIndex = *query.PageIndex
	}
	if query.PageSize != nil {
		pageSize = *query.PageSize
	}
	q.Set("page_index", strconv.Itoa(pageIndex))
	q.Set("page_size", strconv.Itoa(pageSize))
	var out PageResponse[WorkItem]
	err := c.request(ctx, http.MethodGet, pathWorkItems, q, nil, true, &out)
	return out, err
}

func (c *Client) GetWorkItem(ctx context.Context, workItemID string) (WorkItem, error) {
	q := url.Values{"include_public_image_token": {"true"}}
	path := fmt.Sprintf("%s/%s", pathWorkItems, url.PathEscape(workItemID))
	var out WorkItem
	err := c.request(ctx, http.MethodGet, path, q, nil, true, &out)
	return out, err
}

func (c *Client) CreateWorkItem(ctx context.Context, payload WorkItemPayload) (WorkItem, error) {
	var out WorkItem
	err := c.request(ctx, http.MethodPost, pathWorkItems, nil, payload, false, &out)
	return out, err
}

func (c *Client) UpdateWorkItem(ctx context.Context, workItemID string, payload WorkItemPatch) (WorkItem, error) {
	path := fmt.Sprintf("%s/%s", pathWorkItems, url.PathEscape(workItemID))
	var out WorkItem
	err := c.request(ctx, http.MethodPatch, path, nil, payload, false, &out)
	return out, err
}

func (c *Client) ListWorkItemComments(ctx context.Context, workItemID string, pageIndex, pageSize int) (PageResponse[Comment], error) {
	q := url.Values{
		"principal_type": {"work_item"},
		"principal_id":   {workItemID},
	}
	setPage(q, pageIndex, pageSize)
	var out PageResponse[Comment]
	err := c.request(ctx, http.MethodGet, pathComments, q, nil, true, &out)
	return out, err
}

func (c *Client) CreateWorkItemComment(ctx context.Context, workItemID, content string) (Comment, error) {
	body := map[string]any{
		"principal_type": "work_item",
		"principal_id":   workItemID,
		"content":        content,
	}
	var out Comment
	err := c.request(ctx, http.MethodPost, pathComments, nil, body, false, &out)
	return out, err
}

func (c *Client) ExchangeAuthorizationCode(ctx context.Context, code string) (TokenResponse, error) {
	if c.cfg.ClientID == "" || c.cfg.ClientSecret == "" {
		return TokenResponse{}, NewError(CodeConfigMissing, "缺少 PINGCODE_CLIENT_ID / PINGCODE_CLIENT_SECRET，无法用授权码换取用户令牌。")
	}
	return c.tokenRequest(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"code":          {code},
	})
}

func (c *Client) RefreshUserToken(ctx context.Context, refreshToken string) (TokenResponse, error) {
	return c.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body any, isRead bool, out any) error {
	u, err := url.Parse(c.cfg.APIBaseURL + path)
	if err != nil {
		return WrapError(CodeInternalError, "无效 API URL", err)
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return WrapError(CodeInvalidInput, "无法序列化请求体", err)
		}
	}

	maxAttempts := 2
	if isRead {
		maxAttempts = 2 + maxReadRetriesOn429
	}

	var lastErr error
	retried401 := false
	rateRetries := 0
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytesReader(payload))
		if err != nil {
			return WrapError(CodeInternalError, "无法构造请求", err)
		}
		auth, err := c.getAuthorization(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", auth)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil || isTimeout(err) {
				return NewError(CodeUpstreamTimeout, fmt.Sprintf("PingCode API 请求超时：%s %s", method, path))
			}
			return WrapError(CodeUpstreamError, Redact(err.Error()), err)
		}
		text, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return WrapError(CodeUpstreamError, "读取 PingCode 响应失败", readErr)
		}

		if resp.StatusCode == http.StatusUnauthorized && !retried401 && isRead {
			if ok, reauthErr := c.reauthorizeAfter401(ctx); ok {
				retried401 = true
				continue
			} else if reauthErr != nil {
				return Classify(reauthErr)
			}
		}
		if resp.StatusCode == http.StatusUnauthorized && !isRead {
			msg := fmt.Sprintf("PingCode API 鉴权失败：%d（写请求不会自动重试，请检查目标状态后手动重试）", resp.StatusCode)
			return Classify(&APIError{Message: msg, Status: resp.StatusCode, ResponseText: string(text)})
		}
		if isRead && resp.StatusCode == http.StatusTooManyRequests && rateRetries < maxReadRetriesOn429 {
			rateRetries++
			wait := parseRetryAfter(resp.Header.Get("x-pc-retry-after"), resp.Header.Get("Retry-After"))
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return NewError(CodeUpstreamTimeout, "等待限流重试时上下文已取消")
			case <-timer.C:
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = &APIError{
				Message:      fmt.Sprintf("PingCode API 请求失败：%d %s", resp.StatusCode, resp.Status),
				Status:       resp.StatusCode,
				ResponseText: string(text),
			}
			return Classify(lastErr)
		}
		if len(text) == 0 || out == nil {
			return nil
		}
		if err := json.Unmarshal(text, out); err != nil {
			return WrapError(CodeUpstreamError, "无法解析 PingCode JSON 响应", err)
		}
		return nil
	}
	if lastErr != nil {
		return Classify(lastErr)
	}
	return NewError(CodeUpstreamError, fmt.Sprintf("PingCode API 请求未完成：%s %s", method, path))
}

func (c *Client) reauthorizeAfter401(ctx context.Context) (bool, error) {
	if c.store != nil {
		stored, err := c.store.Get()
		if err != nil {
			return false, err
		}
		if stored != nil && stored.RefreshToken != "" {
			refreshed, err := c.RefreshUserToken(ctx, stored.RefreshToken)
			if err != nil {
				return false, err
			}
			_, err = c.store.Update(StoredTokens{
				AccessToken: refreshed.AccessToken,
				TokenType:   firstNonEmpty(refreshed.TokenType, "Bearer"),
				ExpiresAt:   NormalizeExpiresAt(refreshed.ExpiresIn, c.now()),
			})
			return err == nil, err
		}
	}
	if c.cfg.ClientID != "" && c.cfg.ClientSecret != "" {
		c.mu.Lock()
		c.cached = ""
		c.cachedExpiresAt = time.Time{}
		c.mu.Unlock()
		return true, nil
	}
	return false, nil
}

func (c *Client) getAuthorization(ctx context.Context) (string, error) {
	if auth, err := c.tryUserAuthorization(ctx); err != nil {
		return "", err
	} else if auth != "" {
		return auth, nil
	}
	if c.cfg.AccessToken != "" {
		return c.buildAuthorization(c.cfg.AuthScheme, c.cfg.AccessToken)
	}

	c.mu.Lock()
	if c.cached != "" && c.now().Before(c.cachedExpiresAt) {
		auth := c.cached
		c.mu.Unlock()
		return auth, nil
	}
	c.mu.Unlock()

	if c.cfg.ClientID == "" || c.cfg.ClientSecret == "" {
		return "", NewError(CodeAuthRequired, "缺少 PingCode 鉴权配置：请设置 PINGCODE_ACCESS_TOKEN，或设置 PINGCODE_CLIENT_ID 和 PINGCODE_CLIENT_SECRET，或先完成用户登录。")
	}
	token, err := c.tokenRequest(ctx, url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
	})
	if err != nil {
		return "", err
	}
	auth, err := c.buildAuthorization(firstNonEmpty(token.TokenType, c.cfg.AuthScheme), token.AccessToken)
	if err != nil {
		return "", err
	}
	ttl := token.ExpiresIn
	if ttl <= 0 {
		ttl = 7200
	}
	ttl = max64(ttl-60, 60)
	c.mu.Lock()
	c.cached = auth
	c.cachedExpiresAt = c.now().Add(time.Duration(ttl) * time.Second)
	c.mu.Unlock()
	return auth, nil
}

func (c *Client) tryUserAuthorization(ctx context.Context) (string, error) {
	if c.store == nil {
		return "", nil
	}
	stored, err := c.store.Get()
	if err != nil {
		return "", err
	}
	if stored == nil || stored.AccessToken == "" {
		return "", nil
	}
	scheme := firstNonEmpty(stored.TokenType, "Bearer")
	if stored.ExpiresAt == 0 {
		return c.buildAuthorization(scheme, stored.AccessToken)
	}
	if c.now().UnixMilli() < stored.ExpiresAt-60_000 {
		return c.buildAuthorization(scheme, stored.AccessToken)
	}
	if stored.RefreshToken == "" {
		return "", nil
	}
	refreshed, err := c.RefreshUserToken(ctx, stored.RefreshToken)
	if err != nil {
		return "", nil // fall through to next auth source
	}
	updated, err := c.store.Update(StoredTokens{
		AccessToken: refreshed.AccessToken,
		TokenType:   firstNonEmpty(refreshed.TokenType, stored.TokenType),
		ExpiresAt:   NormalizeExpiresAt(refreshed.ExpiresIn, c.now()),
	})
	if err != nil {
		return "", nil
	}
	return c.buildAuthorization(firstNonEmpty(updated.TokenType, scheme), updated.AccessToken)
}

func (c *Client) tokenRequest(ctx context.Context, query url.Values) (TokenResponse, error) {
	u, err := url.Parse(c.cfg.APIBaseURL + pathAuthToken)
	if err != nil {
		return TokenResponse{}, WrapError(CodeInternalError, "无效 Token URL", err)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return TokenResponse{}, WrapError(CodeInternalError, "无法构造 Token 请求", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil || isTimeout(err) {
			return TokenResponse{}, NewError(CodeUpstreamTimeout, "PingCode Token 请求超时。")
		}
		return TokenResponse{}, WrapError(CodeUpstreamError, Redact(err.Error()), err)
	}
	defer resp.Body.Close()
	text, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, WrapError(CodeUpstreamError, "读取 Token 响应失败", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenResponse{}, Classify(&APIError{
			Message:      fmt.Sprintf("PingCode Token 请求失败：%d %s", resp.StatusCode, resp.Status),
			Status:       resp.StatusCode,
			ResponseText: string(text),
		})
	}
	var token TokenResponse
	if len(text) > 0 {
		if err := json.Unmarshal(text, &token); err != nil {
			return TokenResponse{}, WrapError(CodeUpstreamError, "无法解析 Token 响应", err)
		}
	}
	if token.AccessToken == "" {
		return TokenResponse{}, NewError(CodeUpstreamError, "PingCode Token 响应缺少 access_token。")
	}
	return token, nil
}

func (c *Client) buildAuthorization(scheme, token string) (string, error) {
	if token == "" {
		return "", NewError(CodeAuthRequired, "缺少 access token")
	}
	authorization := scheme + " " + token
	for _, r := range authorization {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return "", NewError(CodeInvalidInput, "token 或 PINGCODE_AUTH_SCHEME 包含非 ASCII/不可见字符")
		}
		if r < 0x20 || r > 0x7e {
			return "", NewError(CodeInvalidInput, "token 或 PINGCODE_AUTH_SCHEME 包含非 ASCII/不可见字符")
		}
	}
	return authorization, nil
}

// NormalizeExpiresAt converts expires_in into absolute unix milliseconds.
func NormalizeExpiresAt(expiresIn int64, now time.Time) int64 {
	seconds := expiresIn
	if seconds == 0 {
		seconds = defaultUserTokenTTLSeconds
	}
	if seconds > 1_000_000_000 {
		return seconds * 1000
	}
	return now.UnixMilli() + seconds*1000
}

func setPage(q url.Values, pageIndex, pageSize int) {
	if pageIndex >= 0 {
		q.Set("page_index", strconv.Itoa(pageIndex))
	}
	if pageSize > 0 {
		q.Set("page_size", strconv.Itoa(pageSize))
	}
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func bytesReader(payload []byte) io.Reader {
	if payload == nil {
		return nil
	}
	return bytes.NewReader(payload)
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	if t, ok := err.(timeout); ok && t.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}

func parseRetryAfter(values ...string) time.Duration {
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if secs, err := strconv.Atoi(raw); err == nil && secs >= 0 {
			if secs > 30 {
				secs = 30
			}
			return time.Duration(secs) * time.Second
		}
	}
	return time.Second
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
