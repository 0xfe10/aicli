package pingcodert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/spf13/cobra"
)

const maxAttachmentBytes = 100 << 20

var publicFilePath = regexp.MustCompile(`^/files/public/[a-fA-F0-9]{24}(/origin-url)?$`)

// MaybeRunAttachment handles local file downloads without loading the API spec.
func MaybeRunAttachment(args []string, session Session, selection contextflow.Selection, stdout, stderr io.Writer) (bool, error) {
	if len(args) < 2 || args[1] != "attachment" {
		return false, nil
	}
	root := &cobra.Command{Use: "attachment", Short: "Download description images using the selected PingCode account", SilenceUsage: true, SilenceErrors: true}
	var output, workitem string
	cmd := &cobra.Command{Use: "download <url>", Short: "Download an Atlas description image (maximum 100 MiB)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := downloadAttachment(cmd.Context(), session, selection, args[0], output, workitem)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]string{"path": path})
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Destination file (must not exist); defaults to a unique file in the current directory")
	cmd.Flags().StringVar(&workitem, "workitem", "", "Work item ID or short ID owning the description image (required)")
	_ = cmd.MarkFlagRequired("workitem")
	root.AddCommand(cmd)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args[2:])
	return true, root.Execute()
}

func downloadAttachment(ctx context.Context, session Session, selection contextflow.Selection, rawURL, output, workitem string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !trustedAttachmentURL(u) {
		return "", fmt.Errorf("attachment URL must be https://atlas.pingcode.com/files/public/<file-id> (optionally /origin-url), without query or fragment")
	}
	imageToken, err := workitemImageToken(ctx, session, selection, workitem, u)
	if err != nil {
		return "", err
	}
	return fetchAttachment(ctx, attachmentClient(imageToken), u, imageToken, output)
}

func attachmentClient(imageToken string) *http.Client {
	return &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" || req.URL.User != nil {
			return fmt.Errorf("unsafe attachment redirect")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Referer")
		q := req.URL.Query()
		changed := false
		for key, values := range q {
			for _, value := range values {
				if value == imageToken {
					q.Del(key)
					changed = true
				}
			}
		}
		if changed {
			req.URL.RawQuery = q.Encode()
		}
		return nil
	}}
}

func fetchAttachment(ctx context.Context, client *http.Client, u *url.URL, imageToken, output string) (string, error) {
	query := u.Query()
	query.Set("token", imageToken)
	u.RawQuery = query.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("attachment request failed (transport or redirect)")
	}
	defer resp.Body.Close()
	return saveAttachment(resp, output)
}

func trustedAttachmentURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Host == "atlas.pingcode.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && publicFilePath.MatchString(u.EscapedPath())
}

func saveAttachment(resp *http.Response, output string) (string, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("attachment returned HTTP %d", resp.StatusCode)
	}
	// Sniff bytes as well as headers: Atlas returns JSON business errors with 200.
	prefix := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, prefix)
	prefix = prefix[:n]
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read attachment failed")
	}
	contentType := http.DetectContentType(prefix)
	trimmed := strings.TrimSpace(string(prefix))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
		body, _ := io.ReadAll(io.LimitReader(io.MultiReader(strings.NewReader(string(prefix)), resp.Body), 1<<20))
		var payload struct {
			Code json.RawMessage `json:"code"`
		}
		_ = json.Unmarshal(body, &payload)
		code := strings.Trim(string(payload.Code), "\"")
		if matched, _ := regexp.MatchString(`^[0-9]{1,12}$`, code); matched {
			return "", fmt.Errorf("attachment API returned business error %s; the image token may have expired or lack file access", code)
		}
		return "", fmt.Errorf("attachment returned JSON instead of file content")
	}
	if contentType == "text/html; charset=utf-8" || strings.HasPrefix(contentType, "text/plain") {
		return "", fmt.Errorf("attachment returned text instead of file content")
	}
	if resp.ContentLength > maxAttachmentBytes {
		return "", fmt.Errorf("attachment exceeds 100 MiB limit")
	}
	dir := "."
	if output != "" {
		dir = filepath.Dir(output)
	}
	tmp, err := os.CreateTemp(dir, ".pingcode-attachment-*")
	if err != nil {
		return "", fmt.Errorf("create attachment file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, io.LimitReader(io.MultiReader(strings.NewReader(string(prefix)), resp.Body), maxAttachmentBytes+1))
	if err != nil {
		return "", fmt.Errorf("read attachment failed")
	}
	if size > maxAttachmentBytes {
		return "", fmt.Errorf("attachment exceeds 100 MiB limit")
	}
	if size == 0 {
		return "", fmt.Errorf("attachment is empty")
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("finish attachment file: %w", err)
	}
	if output == "" {
		ext := ".bin"
		switch contentType {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
				ext = exts[0]
			}
		}
		output = "pingcode-" + strings.TrimPrefix(filepath.Base(tmp.Name()), ".pingcode-attachment-") + ext
	}
	// Linking publishes a complete file atomically and refuses to replace an
	// existing destination (including symlinks).
	if err := os.Link(tmp.Name(), output); err != nil {
		return "", fmt.Errorf("save attachment (destination must not exist): %w", err)
	}
	return filepath.Abs(output)
}
