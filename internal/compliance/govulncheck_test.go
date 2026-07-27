package compliance

import (
	"strings"
	"testing"
)

func TestUnapprovedFindingsRejectsUnknown(t *testing.T) {
	allowed := []string{"GO-2026-4740", "GO-2026-4513"}
	found := []string{"GO-2026-4513", "GO-2026-4740", "GO-2026-9999"}
	bad := UnapprovedFindings(found, allowed)
	if len(bad) != 1 || bad[0] != "GO-2026-9999" {
		t.Fatalf("bad = %v", bad)
	}
	if UnapprovedFindings(allowed, allowed) != nil {
		t.Fatal("expected allowlist subset to pass")
	}
}

func TestParseAllowlistAndCollectFindings(t *testing.T) {
	ids, err := ParseAllowlist(strings.NewReader(`# title
| x | y |
` + "```text\nGO-2026-4740\nGO-2026-4513\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "GO-2026-4513" || ids[1] != "GO-2026-4740" {
		t.Fatalf("ids = %v", ids)
	}

	stream := strings.NewReader(`{"config":{}}
{"finding":{"osv":"GO-2026-4740"}}
{"finding":{"osv":"GO-2026-4513"}}
{"finding":{"osv":"GO-2026-4740"}}
`)
	found, err := CollectFindingIDs(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found = %v", found)
	}
	if UnapprovedFindings(found, ids) != nil {
		t.Fatalf("unexpected unapproved: %v", UnapprovedFindings(found, ids))
	}
}

func TestCollectFindingsRejectsInvalidJSON(t *testing.T) {
	_, err := CollectFindingIDs(strings.NewReader("{not-json"))
	if err == nil {
		t.Fatal("expected JSON error")
	}
}
