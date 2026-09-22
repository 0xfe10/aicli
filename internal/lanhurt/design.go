package lanhurt

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type DesignReference struct {
	TeamID    string
	ProjectID string
	DesignID  string
	VersionID string
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type DesignNode struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Type   string `json:"type,omitempty"`
	Bounds *Rect  `json:"bounds,omitempty"`
}

type DesignAsset struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type Design struct {
	ID            string        `json:"design_id"`
	Name          string        `json:"design_name"`
	VersionID     string        `json:"version_id"`
	VersionLabel  string        `json:"version_label,omitempty"`
	SourceType    string        `json:"source_type,omitempty"`
	Canvas        Rect          `json:"canvas"`
	ReferenceSize image.Point   `json:"-"`
	Nodes         []DesignNode  `json:"nodes"`
	Assets        []DesignAsset `json:"assets"`
	RawSHA256     string        `json:"raw_sha256"`
	DDSAvailable  bool          `json:"dds_available"`
	reference     []byte
}

func ParseDesignReference(raw string) (DesignReference, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return DesignReference{}, err
	}
	q := u.Query()
	if i := strings.IndexByte(u.Fragment, '?'); i >= 0 {
		fq, err := url.ParseQuery(u.Fragment[i+1:])
		if err != nil {
			return DesignReference{}, err
		}
		for key, values := range fq {
			if q.Get(key) == "" && len(values) > 0 {
				q.Set(key, values[0])
			}
		}
	}
	ref := DesignReference{TeamID: q.Get("tid"), ProjectID: first(q.Get("pid"), q.Get("project_id")), DesignID: first(q.Get("image_id"), q.Get("design_id")), VersionID: first(q.Get("version_id"), q.Get("versionId"))}
	if ref.ProjectID == "" || ref.DesignID == "" {
		return DesignReference{}, errors.New("Lanhu design URL must contain pid/project_id and image_id/design_id")
	}
	return ref, nil
}

func (c *Client) LoadDesign(ctx context.Context, rawURL string) (Design, error) {
	ref, err := ParseDesignReference(rawURL)
	if err != nil {
		return Design{}, err
	}
	endpoint, _ := url.Parse(baseURL + "/api/project/image")
	q := endpoint.Query()
	q.Set("project_id", ref.ProjectID)
	q.Set("image_id", ref.DesignID)
	q.Set("dds_status", "1")
	if ref.TeamID != "" {
		q.Set("team_id", ref.TeamID)
	}
	endpoint.RawQuery = q.Encode()
	var envelope map[string]any
	if err := c.get(ctx, endpoint.String(), &envelope); err != nil {
		return Design{}, err
	}
	result, err := apiResult(envelope)
	if err != nil {
		return Design{}, err
	}
	if kind := text(result, "type"); kind == "axure" || kind == "pdf" || kind == "word" || kind == "ppt" || kind == "excel" {
		return Design{}, fmt.Errorf("%s is a product document, not a UI design", kind)
	}
	versions, _ := result["versions"].([]any)
	var version map[string]any
	wantedVersion := ref.VersionID
	if wantedVersion == "" || wantedVersion == "latest" {
		wantedVersion = text(result, "latest_version")
	}
	for _, item := range versions {
		candidate, _ := item.(map[string]any)
		if wantedVersion == "" || text(candidate, "id") == wantedVersion {
			version = candidate
			break
		}
	}
	if version == nil {
		return Design{}, errors.New("requested design version was not found")
	}
	jsonURL, referenceURL := text(version, "json_url"), text(version, "url")
	if jsonURL == "" || referenceURL == "" {
		return Design{}, errors.New("design version has no JSON or reference image URL")
	}
	var raw map[string]any
	if err := c.get(ctx, jsonURL, &raw); err != nil {
		return Design{}, fmt.Errorf("download design JSON: %w", err)
	}
	var imageBytes bytes.Buffer
	if err := c.get(ctx, stripOSSProcess(referenceURL), &imageBytes); err != nil {
		return Design{}, fmt.Errorf("download design reference: %w", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(imageBytes.Bytes()))
	if err != nil {
		return Design{}, fmt.Errorf("decode design reference: %w", err)
	}
	rawBytes, _ := json.Marshal(raw)
	design := Design{ID: first(text(result, "id"), ref.DesignID), Name: text(result, "name"), VersionID: text(version, "id"), VersionLabel: text(version, "version_info"), SourceType: text(result, "type"), ReferenceSize: image.Pt(config.Width, config.Height), RawSHA256: digest(rawBytes), reference: imageBytes.Bytes()}
	design.Canvas = Rect{Width: number(result["width"]), Height: number(result["height"])}
	if design.Canvas.Width == 0 || design.Canvas.Height == 0 {
		design.Canvas.Width, design.Canvas.Height = float64(config.Width), float64(config.Height)
	}
	design.Nodes, design.Assets = normalizeDesign(raw)
	design.DDSAvailable = c.ddsAvailable(ctx, design.VersionID)
	return design, nil
}

func (c *Client) ddsAvailable(ctx context.Context, versionID string) bool {
	endpoint := "https://dds.lanhuapp.com/api/dds/image/store_schema_revise?version_id=" + url.QueryEscape(versionID)
	var envelope map[string]any
	if c.get(ctx, endpoint, &envelope) != nil {
		return false
	}
	result, err := apiResult(envelope)
	return err == nil && text(result, "data_resource_url") != ""
}

func normalizeDesign(raw map[string]any) ([]DesignNode, []DesignAsset) {
	nodes := make([]DesignNode, 0)
	assets := make([]DesignAsset, 0)
	seenNodes, seenAssets := map[string]bool{}, map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			id := first(text(current, "id"), text(current, "objectID"), text(current, "do_objectID"), text(current, "layerId"))
			if id != "" && !seenNodes[id] {
				if bounds := boundsOf(current); bounds != nil || text(current, "name") != "" {
					nodes = append(nodes, DesignNode{ID: id, Name: first(text(current, "name"), text(current, "layerName")), Type: first(text(current, "type"), text(current, "_class")), Bounds: bounds})
					seenNodes[id] = true
				}
			}
			for key, child := range current {
				if value, ok := child.(string); ok && assetKey(key) && validAssetURL(value) && !seenAssets[value] {
					assets = append(assets, DesignAsset{ID: digest([]byte(value))[:16], URL: value})
					seenAssets[value] = true
				}
				walk(child)
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	walk(raw)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, assets
}

func boundsOf(value map[string]any) *Rect {
	for _, key := range []string{"frame", "bounds", "rect"} {
		if nested, ok := value[key].(map[string]any); ok {
			if rect := rectOf(nested); rect != nil {
				return rect
			}
		}
	}
	return rectOf(value)
}

func rectOf(value map[string]any) *Rect {
	w, h := number(value["width"]), number(value["height"])
	if w <= 0 || h <= 0 {
		return nil
	}
	return &Rect{X: number(value["x"]), Y: number(value["y"]), Width: w, Height: h}
}

func InspectDesign(design Design, region Rect, output string) (map[string]any, error) {
	if region.Width <= 0 || region.Height <= 0 {
		return nil, errors.New("region width and height must be positive")
	}
	if design.ReferenceSize.X <= 0 || design.ReferenceSize.Y <= 0 || design.ReferenceSize.X > 100_000_000/design.ReferenceSize.Y {
		return nil, errors.New("reference image exceeds the 100 megapixel local processing limit")
	}
	scaleX := float64(design.ReferenceSize.X) / design.Canvas.Width
	scaleY := float64(design.ReferenceSize.Y) / design.Canvas.Height
	x0, y0 := int(region.X*scaleX), int(region.Y*scaleY)
	x1, y1 := int((region.X+region.Width)*scaleX), int((region.Y+region.Height)*scaleY)
	if x0 < 0 || y0 < 0 || x1 > design.ReferenceSize.X || y1 > design.ReferenceSize.Y || x1 <= x0 || y1 <= y0 {
		return nil, errors.New("region is outside the design canvas")
	}
	source, _, err := image.Decode(bytes.NewReader(design.reference))
	if err != nil {
		return nil, err
	}
	crop := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(crop, crop.Bounds(), source, image.Pt(x0, y0), draw.Src)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil && filepath.Dir(output) != "." {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".crop-*.png")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := png.Encode(tmp, crop); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, output); err != nil {
		return nil, err
	}
	nodes := make([]DesignNode, 0)
	for _, node := range design.Nodes {
		if node.Bounds != nil && intersects(*node.Bounds, region) {
			nodes = append(nodes, node)
		}
	}
	return map[string]any{"output": output, "region": region, "nodes": nodes, "node_count": len(nodes)}, nil
}

func (c *Client) ExportDesign(ctx context.Context, design Design, output string) (map[string]any, error) {
	if len(design.Assets) == 0 {
		return nil, errors.New("no downloadable assets were found in the design JSON")
	}
	if len(design.Assets) > 200 {
		return nil, fmt.Errorf("design exposes %d assets; refusing to export more than 200 at once", len(design.Assets))
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil && filepath.Dir(output) != "." {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".assets-*.zip")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	bundle := zip.NewWriter(tmp)
	manifest := struct {
		DesignID  string           `json:"design_id"`
		VersionID string           `json:"version_id"`
		Assets    []map[string]any `json:"assets"`
	}{DesignID: design.ID, VersionID: design.VersionID}
	for index, asset := range design.Assets {
		var data bytes.Buffer
		if err := c.get(ctx, asset.URL, &data); err != nil {
			manifest.Assets = append(manifest.Assets, map[string]any{"id": asset.ID, "error": err.Error()})
			continue
		}
		if data.Len() == 0 {
			manifest.Assets = append(manifest.Assets, map[string]any{"id": asset.ID, "error": "downloaded asset is empty"})
			continue
		}
		mediaType := http.DetectContentType(data.Bytes())
		metadata := map[string]any{"id": asset.ID, "bytes": data.Len(), "sha256": digest(data.Bytes()), "media_type": mediaType}
		if strings.HasPrefix(mediaType, "image/") && mediaType != "image/svg+xml" {
			config, _, decodeErr := image.DecodeConfig(bytes.NewReader(data.Bytes()))
			if decodeErr != nil {
				manifest.Assets = append(manifest.Assets, map[string]any{"id": asset.ID, "error": "invalid raster image"})
				continue
			}
			metadata["width"], metadata["height"] = config.Width, config.Height
		}
		ext := strings.ToLower(filepath.Ext(mustURLPath(asset.URL)))
		if len(ext) > 8 || ext == "" {
			ext = ".bin"
		}
		name := fmt.Sprintf("assets/%03d-%s%s", index+1, asset.ID, ext)
		writer, err := bundle.Create(name)
		if err != nil {
			bundle.Close()
			tmp.Close()
			return nil, err
		}
		if _, err := io.Copy(writer, bytes.NewReader(data.Bytes())); err != nil {
			bundle.Close()
			tmp.Close()
			return nil, err
		}
		metadata["path"] = name
		manifest.Assets = append(manifest.Assets, metadata)
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	writer, err := bundle.Create("manifest.json")
	if err == nil {
		_, err = writer.Write(manifestBytes)
	}
	if closeErr := bundle.Close(); err == nil {
		err = closeErr
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, output); err != nil {
		return nil, err
	}
	succeeded := 0
	for _, asset := range manifest.Assets {
		if asset["path"] != nil {
			succeeded++
		}
	}
	return map[string]any{"output": output, "requested": len(design.Assets), "succeeded": succeeded, "failed": len(design.Assets) - succeeded, "manifest": manifest.Assets}, nil
}

func apiResult(envelope map[string]any) (map[string]any, error) {
	code := fmt.Sprint(envelope["code"])
	if code != "0" && code != "00000" {
		return nil, fmt.Errorf("Lanhu API rejected request: code=%s message=%s", code, first(text(envelope, "msg"), text(envelope, "message")))
	}
	for _, key := range []string{"result", "data"} {
		if result, ok := envelope[key].(map[string]any); ok {
			return result, nil
		}
	}
	return map[string]any{}, nil
}

func stripOSSProcess(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Del("x-oss-process")
	u.RawQuery = q.Encode()
	return u.String()
}

func assetKey(key string) bool {
	key = strings.ToLower(key)
	if strings.Contains(key, "json") || strings.Contains(key, "schema") || strings.Contains(key, "dds") {
		return false
	}
	return strings.Contains(key, "url") || strings.Contains(key, "src") || strings.Contains(key, "image") || strings.Contains(key, "export")
}

func validAssetURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && allowedHost(u.Hostname())
}

func intersects(a, b Rect) bool {
	return a.X < b.X+b.Width && a.X+a.Width > b.X && a.Y < b.Y+b.Height && a.Y+a.Height > b.Y
}

func number(value any) float64 {
	switch value := value.(type) {
	case float64:
		return value
	case json.Number:
		result, _ := value.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(value, 64)
		return result
	default:
		return 0
	}
}

func text(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustURLPath(raw string) string {
	u, _ := url.Parse(raw)
	return u.Path
}
