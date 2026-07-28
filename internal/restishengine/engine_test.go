package restishengine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	restish "github.com/rest-sh/restish/v2"
)

func TestFilterConfigFlags(t *testing.T) {
	got, stripped := FilterConfigFlags([]string{"fns", "--rsh-config", "/tmp/x.json", "note", "get"})
	if !stripped || len(got) != 3 || got[0] != "fns" || got[1] != "note" {
		t.Fatalf("got=%v stripped=%v", got, stripped)
	}
	got, stripped = FilterConfigFlags([]string{"fns", "--rsh-config=/tmp/x.json", "note"})
	if !stripped || len(got) != 2 {
		t.Fatalf("got=%v stripped=%v", got, stripped)
	}
	got, stripped = FilterConfigFlags([]string{"fns", "note", "get", "--", "--rsh-config", "business-value"})
	if stripped || strings.Join(got, "\x00") != strings.Join([]string{"fns", "note", "get", "--", "--rsh-config", "business-value"}, "\x00") {
		t.Fatalf("post-separator args changed: got=%v stripped=%v", got, stripped)
	}
}

func TestForceNoResponseCache(t *testing.T) {
	got := ForceNoResponseCache([]string{"fns", "--rsh-no-cache=false", "note", "get", "--", "--rsh-no-cache=false"})
	want := []string{"fns", "--rsh-no-cache", "note", "get", "--", "--rsh-no-cache=false"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestIsolateResetsEngineConfig(t *testing.T) {
	dir := secureTempDir(t)
	engine := filepath.Join(dir, "engine", "restish.json")
	if err := os.MkdirAll(filepath.Dir(engine), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte(`{"apis":{"fns":{"base_url":"https://evil.example.com"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cli := restish.New()
	restore, err := Isolate(cli, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	data, err := os.ReadFile(engine)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("engine config = %q", data)
	}
	if os.Getenv("RSH_CONFIG") != engine {
		t.Fatalf("RSH_CONFIG = %q", os.Getenv("RSH_CONFIG"))
	}
}

func TestIsolateRejectsSymlinkEngineDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires additional Windows privileges")
	}
	stateDir := secureTempDir(t)
	target := t.TempDir()
	if err := os.Symlink(target, EngineDir(stateDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := Isolate(restish.New(), stateDir); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v", err)
	}
}

func TestIsolateRejectsLooseEngineDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission test")
	}
	stateDir := secureTempDir(t)
	if err := os.Mkdir(EngineDir(stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(EngineDir(stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Isolate(restish.New(), stateDir); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("error = %v", err)
	}
}

func TestIsolateUsesTransientStateWhenPersistentPathCannotBeCreated(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses procfs as a reliably unwritable parent")
	}
	t.Setenv("RSH_CACHE_DIR", "/proc/aicli-cache-test")
	restore, err := Isolate(restish.New(), "/proc/aicli-restish-test")
	if err != nil {
		t.Fatal(err)
	}
	engineFile := os.Getenv("RSH_CONFIG")
	if !strings.HasPrefix(engineFile, os.TempDir()+string(os.PathSeparator)) {
		t.Fatalf("transient config = %q", engineFile)
	}
	if cacheDir := os.Getenv("RSH_CACHE_DIR"); !strings.HasPrefix(cacheDir, os.TempDir()+string(os.PathSeparator)) {
		t.Fatalf("transient cache = %q", cacheDir)
	}
	restore()
	if got := os.Getenv("RSH_CACHE_DIR"); got != "/proc/aicli-cache-test" {
		t.Fatalf("RSH_CACHE_DIR restored to %q", got)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(engineFile))); !os.IsNotExist(err) {
		t.Fatalf("transient state still exists: %v", err)
	}
}

func TestIsolateUsesTransientStateForExistingReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses procfs as a reliably read-only secure directory")
	}
	restore, err := Isolate(restish.New(), "/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	engineFile := os.Getenv("RSH_CONFIG")
	if !strings.HasPrefix(engineFile, os.TempDir()+string(os.PathSeparator)) {
		t.Fatalf("transient config = %q", engineFile)
	}
	restore()
}

func TestIsolateSerializesProcessEnvironment(t *testing.T) {
	restoreFirst, err := Isolate(restish.New(), secureTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	secondDir := secureTempDir(t)
	go func() {
		restoreSecond, err := Isolate(restish.New(), secondDir)
		if err == nil {
			restoreSecond()
		}
		done <- err
	}()
	select {
	case err := <-done:
		restoreFirst()
		t.Fatalf("second isolation was not serialized: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	restoreFirst()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second isolation did not resume")
	}
}

func secureTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}
