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
			body = []byte(`{"info":[{"do_objectID":"node","name":"Button","frame":{"x":2,"y":2,"width":5,"height":4},"exportable":true,"image":{"imageUrl":"https://lanhu.oss-cn-beijing.aliyuncs.com/asset.png"}}]}`)
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

func TestLoadDesignRejectsMismatchedID(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"code":"00000","result":{"id":"other"}}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	client := Client{HTTP: &http.Client{Transport: transport}}
	_, err := client.LoadDesign(context.Background(), "https://lanhuapp.com/web/#/item/project/detail?pid=project&image_id=design")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched design accepted: %v", err)
	}
}

func TestNormalizeObservedSourceCoordinatesAndExportFlags(t *testing.T) {
	assetURL := "https://lanhu.oss-cn-beijing.aliyuncs.com/asset.png"
	fixtures := []struct {
		name, source string
		raw          map[string]any
	}{
		{"figma", "figma", map[string]any{"artboard": map[string]any{"id": "canvas", "frame": map[string]any{"left": 0.0, "top": 0.0, "width": 100.0, "height": 100.0}, "children": []any{map[string]any{"id": "node", "frame": map[string]any{"left": 12.0, "top": 23.0, "width": 30.0, "height": 40.0}, "hasExportImage": true, "image": map[string]any{"imageUrl": assetURL}}, map[string]any{"id": "preview", "frame": map[string]any{"left": 1.0, "top": 1.0, "width": 2.0, "height": 2.0}, "image": map[string]any{"imageUrl": assetURL + "?preview=1"}}}}}},
		{"sketch", "sketch", map[string]any{"info": []any{map[string]any{"do_objectID": "node", "frame": map[string]any{"x": 12.0, "y": 23.0, "width": 30.0, "height": 40.0}, "exportable": true, "image": assetURL}}}},
		{"photoshop", "photoshop", map[string]any{"type": "ps", "board": map[string]any{"id": "canvas", "children": []any{map[string]any{"id": "node", "left": 12.0, "top": 23.0, "right": 42.0, "bottom": 63.0, "isSlice": true, "images": map[string]any{"png": assetURL}}}}}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			nodes, assets, source := normalizeDesign(fixture.raw)
			if source != fixture.source || len(assets) != 1 {
				t.Fatalf("source=%s assets=%+v", source, assets)
			}
			var found *Rect
			for _, node := range nodes {
				if node.ID == "node" {
					found = node.Bounds
				}
			}
			if found == nil || found.X != 12 || found.Y != 23 || found.Width != 30 || found.Height != 40 {
				t.Fatalf("node bounds=%+v nodes=%+v", found, nodes)
			}
		})
	}
}

func TestResolveCanvasRejectsUnverifiedCoordinates(t *testing.T) {
	for _, fixture := range []struct {
		name, source string
		raw          map[string]any
	}{
		{"figma", "figma", map[string]any{"artboard": map[string]any{"frame": map[string]any{"left": 1000.0, "top": 2000.0, "width": 100.0, "height": 50.0}}}},
		{"photoshop", "photoshop", map[string]any{"board": map[string]any{"left": 1000.0, "top": 2000.0, "right": 1100.0, "bottom": 2050.0}}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := resolveCanvas(fixture.raw, fixture.source, 100, 50, image.Pt(200, 100)); err == nil || !strings.Contains(err.Error(), "non-zero origin") {
				t.Fatalf("non-zero canvas accepted: %v", err)
			}
		})
	}
	if _, err := resolveCanvas(map[string]any{}, "sketch", 100, 100, image.Pt(200, 100)); err == nil || !strings.Contains(err.Error(), "aspect ratios") {
		t.Fatalf("mismatched reference accepted: %v", err)
	}
}

func TestExportRejectsHTMLAndBoundsTotalBytes(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<html>expired</html>")), Request: req}, nil
	})
	client := Client{HTTP: &http.Client{Transport: transport}}
	design := Design{ID: "d", VersionID: "v", Assets: []DesignAsset{{ID: "a", URL: "https://lanhu.oss-cn-beijing.aliyuncs.com/asset.png", Kind: "exported_asset"}}}
	result, err := client.ExportDesign(context.Background(), design, filepath.Join(t.TempDir(), "assets.zip"))
	if err != nil || result["succeeded"] != 0 || result["failed"] != 1 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if _, err := nextBundleTotal(maxBundleBytes-1, 2); err == nil {
		t.Fatal("total bundle limit was not enforced")
	}
	for name, test := range map[string]struct {
		data []byte
		url  string
	}{
		"webp": {[]byte("RIFFxxxxWEBP"), "https://lanhuapp.com/a.webp"},
		"avif": {[]byte("xxxxftypavifxxxx"), "https://lanhuapp.com/a.avif"},
		"svg":  {[]byte("<svg"), "https://lanhuapp.com/a.svg"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, _, err := inspectAsset(test.data, test.url); err == nil {
				t.Fatalf("truncated %s was accepted", name)
			}
		})
	}
	if extension, mediaType, _, _, err := inspectAsset([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`), "https://lanhuapp.com/a.svg"); err != nil || extension != ".svg" || mediaType != "image/svg+xml" {
		t.Fatalf("valid SVG extension=%q media=%q err=%v", extension, mediaType, err)
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
