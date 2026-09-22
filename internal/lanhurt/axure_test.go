package lanhurt

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseReferenceAndPages(t *testing.T) {
	ref, err := ParseReference("https://lanhuapp.com/web/#/item/project/product?tid=t&pid=p&docId=d")
	if err != nil || ref.TeamID != "t" || ref.ProjectID != "p" || ref.DocumentID != "d" {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	nodes := []any{map[string]any{"pageName": "Folder", "children": []any{map[string]any{"pageName": "Login", "url": "login.html", "id": "1"}}}}
	pages := appendPages(nil, nodes, "", 0)
	if len(pages) != 1 || pages[0].Path != "Folder/Login" || pages[0].Level != 1 {
		t.Fatalf("pages=%+v", pages)
	}
	if safeRelative("../secret") == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestRenderWithInstalledChromium(t *testing.T) {
	chromium := ""
	for _, candidate := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if path, err := exec.LookPath(candidate); err == nil {
			chromium = path
			break
		}
	}
	if chromium == "" {
		t.Skip("Chromium is not installed")
	}
	dir := t.TempDir()
	page := `<html><body style="margin:0"><div id="top">loading</div><div style="height:1800px"></div><div id="bottom">bottom</div><script>setTimeout(()=>document.getElementById('top').textContent='ready',500)</script></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "screenshot.png")
	t.Setenv("LANHU_CHROMIUM", chromium)
	t.Setenv("LANHU_CHROMIUM_NO_SANDBOX", "1")
	if err := Render(context.Background(), dir, "index.html", output); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("screenshot info=%v err=%v", info, err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Height < 1800 {
		t.Fatalf("full-page screenshot size=%dx%d err=%v", config.Width, config.Height, err)
	}
}

func TestAxureLoadDownloadAndStaleDocumentRecovery(t *testing.T) {
	requests := map[string]int{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Host + req.URL.Path
		requests[key]++
		var body string
		switch key {
		case "lanhuapp.com/api/project/image":
			if req.URL.Query().Get("image_id") == "old" {
				body = `{"code":"10009","msg":"Image not exist"}`
			} else {
				body = `{"code":"00000","result":{"id":"new","name":"Prototype","versions":[{"id":"v1","json_url":"https://cdn.lanhuapp.com/map.json"}]}}`
			}
		case "lanhuapp.com/api/project/product_documents":
			body = `{"code":"00000","result":{"resources":[{"id":"new","type":"axure"}]}}`
		case "cdn.lanhuapp.com/map.json":
			body = `{"sitemap":{"rootNodes":[{"id":"p1","pageName":"Login","url":"login.html"}]},"pages":{"login.html":{"html":{"sign_md5":"html"},"mapping_md5":"mapping"}}}`
		case "axure-file.lanhuapp.com/html":
			body = `<html><head><link data-src="styles.css"></head><body style="display:none; color:red"><img data-src="images/logo.png"><div id="dialog" style="display:none">hidden</div></body></html>`
		case "axure-file.lanhuapp.com/mapping":
			body = `{"images":{"images/logo.png":{"sign_md5":"logo"}}}`
		case "axure-file.lanhuapp.com/logo":
			body = "image"
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewBufferString(body)), Request: req}, nil
	})
	client := &Client{Cookie: "cookie", HTTP: &http.Client{Transport: transport}}
	doc, err := client.LoadDocument(context.Background(), "https://lanhuapp.com/web/#/item/project/product?tid=t&pid=p&docId=old")
	if err != nil || doc.ID != "new" || len(doc.Pages) != 1 {
		t.Fatalf("doc=%+v err=%v", doc, err)
	}
	dir := t.TempDir()
	if err := client.Download(context.Background(), doc, dir); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(dir, "login.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(html, []byte("lanhu_Axure_Mapping_Data")) || bytes.Contains(html, []byte("data-src=")) {
		t.Fatalf("HTML was not patched: %s", html)
	}
	if !bytes.Contains(html, []byte(`href="styles.css"`)) || !bytes.Contains(html, []byte(`id="dialog" style="display:none"`)) || bytes.Contains(html, []byte(`<body style="display:none`)) {
		t.Fatalf("HTML patch changed link or hidden child state: %s", html)
	}
	if requests["lanhuapp.com/api/project/image"] != 2 {
		t.Fatalf("image requests=%d", requests["lanhuapp.com/api/project/image"])
	}
}
