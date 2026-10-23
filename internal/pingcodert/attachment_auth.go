package pingcodert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/restishengine"
	restishauth "github.com/rest-sh/restish/v2/auth"
)

// workitemImageToken exchanges the API bearer for a scoped image preview token.
// Neither credential is returned through CLI output or accepted on the command line.
func workitemImageToken(ctx context.Context, session Session, selection contextflow.Selection, workitem string, image *url.URL) (string, error) {
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, workitem); !matched {
		return "", fmt.Errorf("--workitem must be a work item ID or short ID")
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(session.BaseURL, "/")+"/v2/pjm/workitems/"+url.PathEscape(workitem)+"?include_public_image_token=description", nil)
	if err != nil {
		return "", fmt.Errorf("invalid PingCode API origin")
	}
	ac := restishauth.AuthContext{BaseURL: session.BaseURL, CacheKey: "pingcode:attachments", TokenStore: restishauth.NewTokenCache(restishengine.TokenCachePath(selection.ConfigDir)), HTTPClient: client}
	auth := &ClientCredentialsAuth{Session: session}
	for attempt := 0; attempt < 2; attempt++ {
		ac.Force = attempt > 0
		if err := auth.Authenticate(ctx, req, ac); err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("work item image authorization request failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
		resp.Body.Close()
		if resp.StatusCode == 401 && attempt == 0 {
			continue
		}
		if readErr != nil || len(body) > 8<<20 {
			return "", fmt.Errorf("cannot read work item image authorization")
		}
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("work item image authorization returned HTTP %d", resp.StatusCode)
		}
		var item struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Token       string `json:"public_image_token"`
		}
		if err := json.Unmarshal(body, &item); err != nil || item.ID == "" || item.Token == "" {
			return "", fmt.Errorf("work item API did not return public_image_token for description")
		}
		// A selected work item must actually reference this file.
		id := strings.Split(strings.TrimPrefix(image.Path, "/files/public/"), "/")[0]
		if !strings.Contains(item.Description, "https://atlas.pingcode.com/files/public/"+id) {
			return "", fmt.Errorf("image is not referenced in the selected work item description")
		}
		return item.Token, nil
	}
	return "", fmt.Errorf("work item image authorization failed")
}
