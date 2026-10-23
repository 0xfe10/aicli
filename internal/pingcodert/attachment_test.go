package pingcodert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xfe10/aicli/internal/contextflow"
)

type attachmentTransport func(*http.Request) (*http.Response, error)

func (f attachmentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func imageBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func response(code int, body []byte) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

func TestAttachmentScopedTokenAndSignedRedirect(t *testing.T) {
	const secret = "scoped-image-secret"
	const signed = "https://cdn.example/image?b=%2f&a=one%20two&sig=exact%2Bvalue"
	client := attachmentClient(secret)
	calls := 0
	client.Transport = attachmentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Error("API credential sent to file server")
		}
		if calls == 1 {
			if !trustedAttachmentURL(&url.URL{Scheme: r.URL.Scheme, Host: r.URL.Host, Path: r.URL.Path}) || r.URL.Query().Get("token") != secret {
				t.Error("missing scoped image authorization")
			}
			v := response(302, nil)
			v.Header.Set("Location", signed)
			return v, nil
		}
		if r.URL.String() != signed {
			t.Errorf("signed redirect query changed: %s", r.URL.String())
		}
		if r.Header.Get("Referer") != "" || strings.Contains(r.URL.String(), secret) {
			t.Error("image token leaked to CDN")
		}
		return response(200, imageBytes(t)), nil
	})
	u, _ := url.Parse("https://atlas.pingcode.com/files/public/6a7ad8318402789e499225cd")
	output := filepath.Join(t.TempDir(), "image.png")
	path, err := fetchAttachment(context.Background(), client, u, secret, output)
	if err != nil {
		t.Fatal(err)
	}
	if path != output || calls != 2 {
		t.Fatalf("path=%s calls=%d", path, calls)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, imageBytes(t)) {
		t.Fatal("image bytes changed")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Error("image permissions must be private")
	}
}

func TestAttachmentFailureDoesNotPublishFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"business", 200, `{"code":400401,"message":"Token missing secret"}`},
		{"json", 200, `{"unexpected":"secret"}`},
		{"html", 200, "<html>secret</html>"},
		{"http", 403, "secret"},
		{"empty", 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "image.png")
			_, err := saveAttachment(response(tc.status, []byte(tc.body)), output)
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe error: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failure published file")
			}
			files, _ := os.ReadDir(filepath.Dir(output))
			if len(files) != 0 {
				t.Fatal("failure left partial files")
			}
		})
	}
}

func TestAttachmentDoesNotOverwrite(t *testing.T) {
	output := filepath.Join(t.TempDir(), "image.png")
	os.WriteFile(output, []byte("keep"), 0600)
	if _, err := saveAttachment(response(200, imageBytes(t)), output); err == nil {
		t.Fatal("overwrote destination")
	}
	got, _ := os.ReadFile(output)
	if string(got) != "keep" {
		t.Fatal("destination changed")
	}
}

func TestAttachmentTrustAndRedirectPolicy(t *testing.T) {
	for _, raw := range []string{"http://atlas.pingcode.com/files/public/6a7ad8318402789e499225cd", "https://atlas.pingcode.com.evil/files/public/6a7ad8318402789e499225cd", "https://atlas.pingcode.com/files/public/6a7ad8318402789e499225cd?token=secret", "https://user@atlas.pingcode.com/files/public/6a7ad8318402789e499225cd", "https://atlas.pingcode.com/files/public/../auth", "https://atlas.pingcode.com:444/files/public/6a7ad8318402789e499225cd"} {
		u, _ := url.Parse(raw)
		if trustedAttachmentURL(u) {
			t.Errorf("trusted %s", raw)
		}
	}
	client := attachmentClient("image-secret")
	req, _ := http.NewRequest("GET", "https://cdn.example/image?token=image-secret&signature=abc", nil)
	req.Header.Set("Authorization", "Bearer api-secret")
	req.Header.Set("Referer", "https://atlas.pingcode.com/?token=image-secret")
	if err := client.CheckRedirect(req, []*http.Request{{}}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "" || req.URL.Query().Get("token") != "" {
		t.Fatal("credential leak")
	}
	req.URL.Scheme = "http"
	if client.CheckRedirect(req, []*http.Request{{}}) == nil {
		t.Fatal("accepted TLS downgrade")
	}
	req.URL.Scheme = "https"
	if client.CheckRedirect(req, make([]*http.Request, 10)) == nil {
		t.Fatal("accepted redirect loop")
	}
}

func TestWorkitemImageTokenUsesSelectedSession(t *testing.T) {
	const file = "https://atlas.pingcode.com/files/public/6a7ad8318402789e499225cd"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/pjm/workitems/work-1" || r.URL.Query().Get("include_public_image_token") != "description" {
			t.Error("wrong token request")
		}
		if r.Header.Get("Authorization") != "Bearer account-secret" {
			t.Error("wrong context credential")
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "work-1", "description": `<img src="` + file + `">`, "public_image_token": "image-secret"})
	}))
	defer server.Close()
	selection := contextflow.Selection{ConfigDir: t.TempDir()}
	session := Session{BaseURL: server.URL, HasCredentials: true, Credentials: Credentials{Mode: AuthModeToken, AccessToken: "account-secret"}}
	u, _ := url.Parse(file)
	token, err := workitemImageToken(context.Background(), session, selection, "work-1", u)
	if err != nil || token != "image-secret" {
		t.Fatalf("failed to obtain scoped image token: %v", err)
	}
	u.Path = "/files/public/aaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := workitemImageToken(context.Background(), session, selection, "work-1", u); err == nil {
		t.Fatal("accepted unrelated image")
	}
}

func TestAttachmentTransportErrorHidesSignedURL(t *testing.T) {
	client := attachmentClient("image-secret")
	client.Transport = attachmentTransport(func(r *http.Request) (*http.Response, error) { return nil, fmt.Errorf("failed %s", r.URL.String()) })
	u, _ := url.Parse("https://atlas.pingcode.com/files/public/6a7ad8318402789e499225cd")
	_, err := fetchAttachment(context.Background(), client, u, "image-secret", filepath.Join(t.TempDir(), "image.png"))
	if err == nil || strings.Contains(err.Error(), "image-secret") || strings.Contains(err.Error(), "token=") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestAttachmentSizeLimit(t *testing.T) {
	r := response(200, imageBytes(t))
	r.ContentLength = maxAttachmentBytes + 1
	output := filepath.Join(t.TempDir(), "image.png")
	if _, err := saveAttachment(r, output); err == nil {
		t.Fatal("accepted oversized attachment")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("published oversized file")
	}
}
