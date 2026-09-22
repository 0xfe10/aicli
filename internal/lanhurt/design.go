package lanhurt

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxBundleBytes = 256 << 20

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
	ID   string `json:"id"`
	URL  string `json:"url"`
	Kind string `json:"kind"`
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
	if id := text(result, "id"); id != "" && id != ref.DesignID {
		return Design{}, errors.New("returned design does not match the requested ID")
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
	design := Design{ID: first(text(result, "id"), ref.DesignID), Name: text(result, "name"), VersionID: text(version, "id"), VersionLabel: text(version, "version_info"), ReferenceSize: image.Pt(config.Width, config.Height), RawSHA256: digest(rawBytes), reference: imageBytes.Bytes()}
	design.Nodes, design.Assets, design.SourceType = normalizeDesign(raw)
	design.Canvas, err = resolveCanvas(raw, design.SourceType, number(result["width"]), number(result["height"]), design.ReferenceSize)
	if err != nil {
		return Design{}, err
	}
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

func normalizeDesign(raw map[string]any) ([]DesignNode, []DesignAsset, string) {
	sourceType := designSourceType(raw)
	nodes := make([]DesignNode, 0)
	assets := make([]DesignAsset, 0)
	seenNodes, seenAssets := map[string]bool{}, map[string]bool{}
	photoshopExports := map[string]bool{}
	if sourceType == "photoshop" {
		if sourceAssets, ok := raw["assets"].([]any); ok {
			for _, item := range sourceAssets {
				asset, _ := item.(map[string]any)
				if id := sourceID(asset); id != "" && (flag(asset["isAsset"]) || flag(asset["isSlice"])) {
					photoshopExports[id] = true
				}
			}
		}
	}
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case map[string]any:
			id := sourceID(current)
			if id != "" && !seenNodes[id] {
				if bounds := boundsOf(current, sourceType); bounds != nil || text(current, "name") != "" {
					nodes = append(nodes, DesignNode{ID: id, Name: first(text(current, "name"), text(current, "layerName")), Type: first(text(current, "type"), text(current, "_class")), Bounds: bounds})
					seenNodes[id] = true
				}
			}
			exported := flag(current["exportable"]) || flag(current["hasExportImage"]) || flag(current["isAsset"]) || flag(current["isSlice"]) || photoshopExports[id]
			if exported {
				for _, field := range []string{"image", "images"} {
					for _, value := range assetURLs(current[field]) {
						if !seenAssets[value] {
							assets = append(assets, DesignAsset{ID: digest([]byte(value))[:16], URL: value, Kind: "exported_asset"})
							seenAssets[value] = true
						}
					}
				}
			}
			for _, field := range []string{"layers", "children"} {
				if child := current[field]; child != nil {
					walk(child)
				}
			}
		case []any:
			for _, child := range current {
				walk(child)
			}
		}
	}
	switch sourceType {
	case "figma":
		walk(raw["artboard"])
	case "photoshop":
		walk(raw["board"])
	case "sketch":
		walk(raw["info"])
	default:
		walk(raw["layers"])
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, assets, sourceType
}

func designSourceType(raw map[string]any) string {
	meta, _ := raw["meta"].(map[string]any)
	host, _ := meta["host"].(map[string]any)
	if strings.EqualFold(text(host, "name"), "figma") || raw["artboard"] != nil {
		return "figma"
	}
	if strings.EqualFold(text(raw, "type"), "ps") || strings.EqualFold(text(raw, "type"), "photoshop") || raw["board"] != nil {
		return "photoshop"
	}
	if _, ok := raw["info"].([]any); ok {
		return "sketch"
	}
	return "unknown"
}

func resolveCanvas(raw map[string]any, sourceType string, apiWidth, apiHeight float64, reference image.Point) (Rect, error) {
	canvas := Rect{Width: apiWidth, Height: apiHeight}
	if source := sourceCanvas(raw, sourceType); source != nil {
		if source.X != 0 || source.Y != 0 {
			return Rect{}, errors.New("source canvas has a non-zero origin whose image mapping is not verified")
		}
		canvas = *source
	}
	if canvas.Width <= 0 || canvas.Height <= 0 {
		return Rect{}, errors.New("source does not provide a reliable canvas size")
	}
	if reference.X <= 0 || reference.Y <= 0 {
		return Rect{}, errors.New("reference image has invalid dimensions")
	}
	ratioError := math.Abs((float64(reference.X)/float64(reference.Y))/(canvas.Width/canvas.Height) - 1)
	if ratioError > 0.01 {
		return Rect{}, errors.New("reference image and source canvas aspect ratios do not match")
	}
	return canvas, nil
}

func sourceCanvas(raw map[string]any, sourceType string) *Rect {
	var candidate map[string]any
	switch sourceType {
	case "figma":
		candidate, _ = raw["artboard"].(map[string]any)
	case "photoshop":
		candidate, _ = raw["board"].(map[string]any)
	case "sketch":
		items, _ := raw["info"].([]any)
		artboardID := fmt.Sprint(raw["ArtboardID"])
		var fallback map[string]any
		fallbacks := 0
		for _, item := range items {
			layer, _ := item.(map[string]any)
			kind := first(text(layer, "ddsType"), text(layer, "type"))
			matchesID := artboardID != "<nil>" && artboardID != "" && sourceID(layer) == artboardID
			if matchesID {
				candidate = layer
				break
			}
			if kind == "artboard-group" || kind == "artboard" || kind == "artboardSection" {
				fallback, fallbacks = layer, fallbacks+1
			}
		}
		if candidate == nil && fallbacks == 1 {
			candidate = fallback
		}
	}
	if candidate == nil {
		return nil
	}
	return boundsOf(candidate, sourceType)
}

func sourceID(value map[string]any) string {
	for _, key := range []string{"id", "objectID", "do_objectID", "layerId"} {
		switch id := value[key].(type) {
		case string:
			if id != "" {
				return id
			}
		case float64:
			if math.IsNaN(id) || math.IsInf(id, 0) {
				continue
			}
			return strconv.FormatFloat(id, 'f', -1, 64)
		}
	}
	return ""
}

func boundsOf(value map[string]any, sourceType string) *Rect {
	fields := []string{"frame", "realFrame", "absoluteBoundingBox", "bounds", ""}
	if sourceType != "figma" {
		fields = []string{"", "frame", "bounds", "layerOriginFrame", "realFrame", "absoluteBoundingBox"}
	}
	for _, key := range fields {
		nested := value
		if key != "" {
			nested, _ = value[key].(map[string]any)
		}
		if nested != nil {
			if rect := rectOf(nested); rect != nil {
				return rect
			}
		}
	}
	return nil
}

func rectOf(value map[string]any) *Rect {
	x, hasX := finiteNumber(value["x"])
	if !hasX {
		x, hasX = finiteNumber(value["left"])
	}
	y, hasY := finiteNumber(value["y"])
	if !hasY {
		y, hasY = finiteNumber(value["top"])
	}
	w, hasWidth := finiteNumber(value["width"])
	h, hasHeight := finiteNumber(value["height"])
	if !hasWidth {
		if right, ok := finiteNumber(value["right"]); ok && hasX {
			w, hasWidth = right-x, true
		}
	}
	if !hasHeight {
		if bottom, ok := finiteNumber(value["bottom"]); ok && hasY {
			h, hasHeight = bottom-y, true
		}
	}
	if !hasX || !hasY || !hasWidth || !hasHeight || w < 0 || h < 0 {
		return nil
	}
	return &Rect{X: x, Y: y, Width: w, Height: h}
}

func flag(value any) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	result, ok := finiteNumber(value)
	return ok && result == 1
}

func assetURLs(value any) []string {
	if raw, ok := value.(string); ok {
		if validAssetURL(raw) {
			return []string{raw}
		}
		return nil
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	keys := []string{"imageUrl", "png_xxxhd", "png", "url", "svgUrl", "svg", "webp", "jpeg", "jpg"}
	result, seen := make([]string, 0), map[string]bool{}
	for _, key := range keys {
		if raw, ok := mapping[key].(string); ok && validAssetURL(raw) && !seen[raw] {
			result, seen[raw] = append(result, raw), true
		}
	}
	return result
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
	totalBytes := 0
	for index, asset := range design.Assets {
		var data bytes.Buffer
		if err := c.get(ctx, asset.URL, &data); err != nil {
			manifest.Assets = append(manifest.Assets, map[string]any{"id": asset.ID, "error": err.Error()})
			continue
		}
		nextTotal, sizeErr := nextBundleTotal(totalBytes, data.Len())
		if sizeErr != nil {
			bundle.Close()
			tmp.Close()
			return nil, sizeErr
		}
		totalBytes = nextTotal
		ext, mediaType, width, height, inspectErr := inspectAsset(data.Bytes(), asset.URL)
		if inspectErr != nil {
			manifest.Assets = append(manifest.Assets, map[string]any{"id": asset.ID, "error": inspectErr.Error()})
			continue
		}
		metadata := map[string]any{"id": asset.ID, "kind": asset.Kind, "bytes": data.Len(), "sha256": digest(data.Bytes()), "media_type": mediaType}
		if width > 0 && height > 0 {
			metadata["width"], metadata["height"] = width, height
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

func nextBundleTotal(total, next int) (int, error) {
	if next < 0 || total > maxBundleBytes-next {
		return total, fmt.Errorf("asset bundle exceeds the %d MiB total download limit", maxBundleBytes>>20)
	}
	return total + next, nil
}

func inspectAsset(data []byte, rawURL string) (string, string, int, int, error) {
	if len(data) == 0 {
		return "", "", 0, 0, errors.New("downloaded asset is empty")
	}
	mediaType := strings.Split(http.DetectContentType(data), ";")[0]
	extension := ""
	switch {
	case mediaType == "image/png":
		extension = ".png"
	case mediaType == "image/jpeg":
		extension = ".jpg"
	case mediaType == "image/gif":
		extension = ".gif"
	case validSVG(data):
		mediaType, extension = "image/svg+xml", ".svg"
	default:
		return "", mediaType, 0, 0, fmt.Errorf("unsupported or invalid image content (%s)", mediaType)
	}
	declared := strings.ToLower(filepath.Ext(mustURLPath(rawURL)))
	aliases := map[string]string{".jpeg": ".jpg"}
	if alias := aliases[declared]; alias != "" {
		declared = alias
	}
	if declared != "" && declared != extension && declared != ".bin" {
		return "", mediaType, 0, 0, fmt.Errorf("asset extension %s does not match downloaded %s content", declared, extension)
	}
	if extension == ".png" || extension == ".jpg" || extension == ".gif" {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width <= 0 || config.Height <= 0 {
			return "", mediaType, 0, 0, errors.New("invalid raster image")
		}
		return extension, mediaType, config.Width, config.Height, nil
	}
	return extension, mediaType, 0, 0, nil
}

func validSVG(data []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, root := 0, false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return root && depth == 0
		}
		if err != nil {
			return false
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if root || token.Name.Local != "svg" {
					return false
				}
				root = true
			}
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				return false
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return false
			}
		}
	}
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

func validAssetURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && allowedHost(u.Hostname())
}

func intersects(a, b Rect) bool {
	return a.X < b.X+b.Width && a.X+a.Width > b.X && a.Y < b.Y+b.Height && a.Y+a.Height > b.Y
}

func number(value any) float64 {
	result, _ := finiteNumber(value)
	return result
}

func finiteNumber(value any) (float64, bool) {
	var result float64
	switch value := value.(type) {
	case float64:
		result = value
	case json.Number:
		var err error
		result, err = value.Float64()
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		result, err = strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return result, !math.IsNaN(result) && !math.IsInf(result, 0)
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
