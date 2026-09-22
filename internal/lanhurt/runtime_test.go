package lanhurt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xfe10/aicli/internal/contextflow"
)

func TestEmbeddedSpecGeneratesAllCommandGroups(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	selection := contextflow.Selection{Name: contextflow.DefaultName, ConfigDir: dir, CacheDir: filepath.Join(dir, "cache")}
	t.Setenv("RSH_CACHE_DIR", selection.CacheDir)
	cli, err := NewCLI(Session{Cookie: "cookie", DDSCookie: "cookie", HasCredentials: true}, selection, "test", "commit")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	if err := RunCLI(cli, []string{"lanhu", "--help"}, selection); err != nil {
		t.Fatalf("run help: %v\nstderr=%s", err, stderr.String())
	}
	for _, group := range []string{"project", "design", "dds"} {
		if !strings.Contains(stdout.String(), group) {
			t.Errorf("root help missing %q", group)
		}
	}
	info, err := os.Stat(filepath.Join(dir, "engine", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("embedded spec mode=%v", info.Mode().Perm())
	}
}
