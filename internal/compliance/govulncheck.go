package compliance

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// FindingEvent is the subset of govulncheck -json output used for gating.
type FindingEvent struct {
	Finding *struct {
		OSV string `json:"osv"`
	} `json:"finding"`
}

// LoadAllowlistIDs reads accepted OSV IDs from the markdown allowlist file.
// It accepts either a fenced ```text block of IDs or bare GO-* lines.
func LoadAllowlistIDs(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseAllowlist(file)
}

// ParseAllowlist extracts OSV IDs from allowlist markdown.
func ParseAllowlist(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	inFence := false
	seen := map[string]bool{}
	var ids []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if strings.HasPrefix(line, "GO-") {
			id := strings.Fields(line)[0]
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			continue
		}
		if inFence && line != "" && strings.HasPrefix(line, "GO-") {
			id := strings.Fields(line)[0]
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

// CollectFindingIDs reads govulncheck -json stream and returns unique OSV IDs.
func CollectFindingIDs(r io.Reader) ([]string, error) {
	dec := json.NewDecoder(r)
	seen := map[string]bool{}
	var ids []string
	for {
		var event FindingEvent
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode govulncheck JSON: %w", err)
		}
		if event.Finding == nil {
			continue
		}
		id := strings.TrimSpace(event.Finding.OSV)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// UnapprovedFindings returns found IDs that are not in the allowlist.
func UnapprovedFindings(found, allowed []string) []string {
	allow := map[string]bool{}
	for _, id := range allowed {
		allow[id] = true
	}
	var bad []string
	for _, id := range found {
		if !allow[id] {
			bad = append(bad, id)
		}
	}
	return bad
}
