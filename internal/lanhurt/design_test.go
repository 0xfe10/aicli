package lanhurt

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowRoutingLeavesGeneratedDesignCommandsToRestish(t *testing.T) {
	handled, err := MaybeRunWorkflow([]string{"design", "list", "project"}, Session{}, io.Discard)
	if handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var help bytes.Buffer
	handled, err = MaybeRunWorkflow([]string{"design", "overview", "--help"}, Session{}, &help)
	if !handled || err != nil || !strings.Contains(help.String(), "lanhu design overview") {
		t.Fatalf("handled=%v help=%q err=%v", handled, help.String(), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestDesignOverviewInspectAndExport(t *testing.T) {
	imageBytes := testPNG(t, 20, 10)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch req.URL.Host + req.URL.Path {
		case "lanhuapp.com/api/project/image":
			if req.Header.Get("Cookie") != "cookie" {
				t.Fatalf("Lanhu Cookie=%q", req.Header.Get("Cookie"))
			}
			body = []byte(`{"code":"00000","result":{"id":"design","name":"Home","type":"sketch","width":20,"height":10,"versions":[{"id":"v1","version_info":"1","json_url":"https://cdn.lanhuapp.com/raw.json","url":"https://lanhu.oss-cn-beijing.aliyuncs.com/reference.png"}]}}`)
		case "cdn.lanhuapp.com/raw.json":
			if req.Header.Get("Cookie") != "" {
				t.Fatal("cookie leaked to signed JSON host")
			}
			body = []byte(`{"layers":[{"id":"node","name":"Button","frame":{"x":2,"y":2,"width":5,"height":4},"image_url":"https://lanhu.oss-cn-beijing.aliyuncs.com/asset.png"}]}`)
		case "lanhu.oss-cn-beijing.aliyuncs.com/reference.png", "lanhu.oss-cn-beijing.aliyuncs.com/asset.png":
			body = imageBytes
		case "dds.lanhuapp.com/api/dds/image/store_schema_revise":
			if req.Header.Get("Cookie") != "dds" {
				t.Fatalf("DDS Cookie=%q", req.Header.Get("Cookie"))
			}
			body = []byte(`{"code":"00000","data":{"data_resource_url":"https://cdn.lanhuapp.com/dds.json"}}`)
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})
	client := &Client{Cookie: "cookie", DDSCookie: "dds", HTTP: &http.Client{Transport: transport}}
	design, err := client.LoadDesign(context.Background(), "https://lanhuapp.com/web/#/item/project/detail?pid=project&image_id=design")
	if err != nil {
		t.Fatal(err)
	}
	if design.ID != "design" || len(design.Nodes) != 1 || len(design.Assets) != 1 || !design.DDSAvailable {
		t.Fatalf("design=%+v", design)
	}
	crop := filepath.Join(t.TempDir(), "crop.png")
	result, err := InspectDesign(design, Rect{X: 1, Y: 1, Width: 10, Height: 8}, crop)
	if err != nil || result["node_count"] != 1 {
		t.Fatalf("inspect=%v err=%v", result, err)
	}
	if _, err := os.Stat(crop); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "assets.zip")
	if _, err := client.ExportDesign(context.Background(), design, bundle); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	names := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	if strings.Join(names, ",") != "assets/001-"+design.Assets[0].ID+".png,manifest.json" {
		t.Fatalf("zip entries=%v", names)
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.Set(x, y, color.RGBA{R: 50, G: 100, B: 150, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
