package lanhurt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func MaybeRunWorkflow(args []string, session Session, stdout io.Writer) (bool, error) {
	if len(args) < 2 || (args[0] != "axure" && args[0] != "design") {
		return false, nil
	}
	if args[0] == "axure" && (args[1] == "--help" || args[1] == "-h") {
		_, err := fmt.Fprint(stdout, "Usage:\n  lanhu axure pages <lanhu-url>\n  lanhu axure download <lanhu-url> <output-dir>\n  lanhu axure render <download-dir> <page.html> <output.png>\n")
		return true, err
	}
	if args[0] == "design" && args[1] != "overview" && args[1] != "inspect" && args[1] != "export" {
		return false, nil
	}
	if args[0] == "design" && len(args) == 3 && (args[2] == "--help" || args[2] == "-h") {
		usage := map[string]string{
			"overview": "lanhu design overview <lanhu-url>",
			"inspect":  "lanhu design inspect <lanhu-url> --region x,y,width,height --output crop.png",
			"export":   "lanhu design export <lanhu-url> --output assets.zip",
		}
		_, err := fmt.Fprintf(stdout, "Usage: %s\n", usage[args[1]])
		return true, err
	}
	if !session.HasCredentials {
		return true, fmt.Errorf("Lanhu authentication is not configured; run %q or set LANHU_COOKIE", "lanhu auth login --mode cookie")
	}
	client := &Client{Cookie: session.Cookie, DDSCookie: session.DDSCookie}
	ctx := context.Background()
	if args[0] == "design" {
		return true, runDesign(ctx, client, args[1:], stdout)
	}
	switch args[1] {
	case "pages":
		if len(args) != 3 {
			return true, fmt.Errorf("usage: lanhu axure pages <lanhu-url>")
		}
		doc, err := client.LoadDocument(ctx, args[2])
		if err != nil {
			return true, err
		}
		return true, writeJSON(stdout, doc)
	case "download":
		if len(args) != 4 {
			return true, fmt.Errorf("usage: lanhu axure download <lanhu-url> <output-dir>")
		}
		doc, err := client.LoadDocument(ctx, args[2])
		if err != nil {
			return true, err
		}
		if err := client.Download(ctx, doc, args[3]); err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(stdout, "%s\n", args[3])
		return true, err
	case "render":
		if len(args) != 5 {
			return true, fmt.Errorf("usage: lanhu axure render <download-dir> <page.html> <output.png>")
		}
		if err := Render(ctx, args[2], args[3], args[4]); err != nil {
			return true, err
		}
		_, err := fmt.Fprintf(stdout, "%s\n", args[4])
		return true, err
	default:
		return true, fmt.Errorf("unknown axure command %q", args[1])
	}
}

func runDesign(ctx context.Context, client *Client, args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: lanhu design overview|inspect|export <lanhu-url> ...")
	}
	design, err := client.LoadDesign(ctx, args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "overview":
		return writeJSON(stdout, map[string]any{"design_id": design.ID, "design_name": design.Name, "version_id": design.VersionID, "version_label": design.VersionLabel, "source_type": design.SourceType, "canvas": design.Canvas, "reference_size": map[string]int{"width": design.ReferenceSize.X, "height": design.ReferenceSize.Y}, "node_count": len(design.Nodes), "asset_count": len(design.Assets), "raw_sha256": design.RawSHA256, "dds_available": design.DDSAvailable})
	case "inspect":
		if len(args) != 6 || args[2] != "--region" || args[4] != "--output" {
			return fmt.Errorf("usage: lanhu design inspect <lanhu-url> --region x,y,width,height --output crop.png")
		}
		region, err := parseRegion(args[3])
		if err != nil {
			return err
		}
		result, err := InspectDesign(design, region, args[5])
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	case "export":
		if len(args) != 4 || args[2] != "--output" {
			return fmt.Errorf("usage: lanhu design export <lanhu-url> --output assets.zip")
		}
		result, err := client.ExportDesign(ctx, design, args[3])
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("unknown design command %q", args[0])
	}
}

func parseRegion(raw string) (Rect, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return Rect{}, fmt.Errorf("region must be x,y,width,height")
	}
	values := make([]float64, 4)
	for index, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return Rect{}, fmt.Errorf("invalid region %q", raw)
		}
		values[index] = value
	}
	return Rect{X: values[0], Y: values[1], Width: values[2], Height: values[3]}, nil
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
