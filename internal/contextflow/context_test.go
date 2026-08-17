package contextflow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveArgsPrecedenceAndPaths(t *testing.T) {
	configHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("OZON_CONTEXT", "")
	manager := New("ozon", "OZON_CONTEXT")

	selection, args, err := manager.ResolveArgs([]string{"ozon", "seller-info"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != DefaultName || selection.Source != SourceDefault {
		t.Fatalf("selection = %#v", selection)
	}
	if selection.ConfigDir != filepath.Join(configHome, "aicli", "ozon") || selection.CacheDir != filepath.Join(cacheHome, "aicli", "ozon") {
		t.Fatalf("default paths = %#v", selection)
	}
	if !reflect.DeepEqual(args, []string{"ozon", "seller-info"}) {
		t.Fatalf("args = %#v", args)
	}

	if err := manager.Use("saved"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OZON_CONTEXT", "environment")
	selection, _, err = manager.ResolveArgs([]string{"ozon", "seller-info"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != "environment" || selection.Source != SourceEnv {
		t.Fatalf("environment selection = %#v", selection)
	}

	selection, args, err = manager.ResolveArgs([]string{"ozon", "--context", "flagged", "seller-info"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != "flagged" || selection.Source != SourceFlag {
		t.Fatalf("flag selection = %#v", selection)
	}
	if !reflect.DeepEqual(args, []string{"ozon", "seller-info"}) {
		t.Fatalf("filtered args = %#v", args)
	}
	wantConfig := filepath.Join(configHome, "aicli", "ozon", "contexts", "flagged")
	wantCache := filepath.Join(cacheHome, "aicli", "ozon", "contexts", "flagged")
	if selection.ConfigDir != wantConfig || selection.CacheDir != wantCache {
		t.Fatalf("named paths = %#v", selection)
	}
}

func TestUseResolveAndList(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("FNS_CONTEXT", "")
	manager := New("fns", "FNS_CONTEXT")
	if err := manager.Use("personal"); err != nil {
		t.Fatal(err)
	}
	selection, _, err := manager.ResolveArgs([]string{"fns"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != "personal" || selection.Source != SourceCurrent {
		t.Fatalf("selection = %#v", selection)
	}
	names, err := manager.List(selection.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"default", "personal"}) {
		t.Fatalf("contexts = %#v", names)
	}
	if runtimePermsSupported() {
		assertPerm(t, filepath.Join(configHome, "aicli", "fns", "current-context"), 0o600)
		assertPerm(t, selection.ConfigDir, 0o700)
	}
}

func TestMaybeRunContextCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PINGCODE_CONTEXT", "")
	manager := New("pingcode", "PINGCODE_CONTEXT")
	selection := manager.DefaultSelection()

	var stdout, stderr bytes.Buffer
	handled, err := MaybeRun([]string{"pingcode", "context", "use", "work"}, manager, selection, &stdout, &stderr)
	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	selection, _, err = manager.ResolveArgs([]string{"pingcode"})
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	handled, err = MaybeRun([]string{"pingcode", "context", "list"}, manager, selection, &stdout, &stderr)
	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	var report struct {
		Current  string   `json:"current"`
		Contexts []string `json:"contexts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Current != "work" || !reflect.DeepEqual(report.Contexts, []string{"default", "work"}) {
		t.Fatalf("report = %#v", report)
	}
}

func TestRejectsInvalidNamesAndLateFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	manager := New("ozon", "OZON_CONTEXT")
	for _, name := range []string{"", "../other", "bad/name", " bad", strings.Repeat("a", 65)} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("ValidateName(%q) succeeded", name)
		}
	}
	for _, args := range [][]string{
		{"ozon", "seller-info", "--context", "late"},
		{"ozon", "--rsh-output-format", "json", "--context", "late", "seller-info"},
	} {
		if _, _, err := manager.ResolveArgs(args); err == nil || !strings.Contains(err.Error(), "must appear before") {
			t.Fatalf("ResolveArgs(%#v) error = %v", args, err)
		}
	}
	literalArgs := []string{"ozon", "seller-info", "--", "--context"}
	selection, filtered, err := manager.ResolveArgs(literalArgs)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Name != DefaultName || !reflect.DeepEqual(filtered, literalArgs) {
		t.Fatalf("literal --context changed selection or args: %#v %#v", selection, filtered)
	}
}

func TestPrepareDefaultRejectsSymlinkedStateRoots(t *testing.T) {
	for _, envName := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Run(envName, func(t *testing.T) {
			configHome := t.TempDir()
			cacheHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configHome)
			t.Setenv("XDG_CACHE_HOME", cacheHome)
			home := configHome
			if envName == "XDG_CACHE_HOME" {
				home = cacheHome
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(home, "aicli")); err != nil {
				t.Fatal(err)
			}
			manager := New("ozon", "OZON_CONTEXT")
			selection, _, err := manager.ResolveArgs([]string{"ozon", "--context", "default"})
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Prepare(selection); err == nil || !strings.Contains(err.Error(), "not a symlink") {
				t.Fatalf("Prepare error = %v", err)
			}
		})
	}
}

func TestRejectsSymlinkedProjectDirectory(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := os.Symlink(t.TempDir(), filepath.Join(configHome, "aicli")); err != nil {
		t.Fatal(err)
	}
	manager := New("ozon", "OZON_CONTEXT")
	if _, _, err := manager.ResolveArgs([]string{"ozon"}); err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("ResolveArgs error = %v", err)
	}
}

func TestRejectsSymlinkedServiceDirectory(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	target := t.TempDir()
	serviceParent := filepath.Join(configHome, "aicli")
	if err := os.MkdirAll(serviceParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(serviceParent, "ozon")); err != nil {
		t.Fatal(err)
	}
	manager := New("ozon", "OZON_CONTEXT")
	if _, _, err := manager.ResolveArgs([]string{"ozon"}); err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("ResolveArgs error = %v", err)
	}
}

func TestPrepareRejectsSymlinkedContextsDirectory(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	serviceRoot := filepath.Join(configHome, "aicli", "ozon")
	if err := os.MkdirAll(serviceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(serviceRoot, "contexts")); err != nil {
		t.Fatal(err)
	}
	manager := New("ozon", "OZON_CONTEXT")
	selection, _, err := manager.ResolveArgs([]string{"ozon", "--context", "seller-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Prepare(selection); err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("Prepare error = %v", err)
	}
}

func TestPrepareCreatesPrivateCacheContext(t *testing.T) {
	configHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	manager := New("ozon", "OZON_CONTEXT")
	selection, _, err := manager.ResolveArgs([]string{"ozon", "--context", "seller-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Prepare(selection); err != nil {
		t.Fatal(err)
	}
	if runtimePermsSupported() {
		assertPerm(t, filepath.Join(cacheHome, "aicli", "ozon"), 0o700)
		assertPerm(t, filepath.Join(cacheHome, "aicli", "ozon", "contexts"), 0o700)
		assertPerm(t, selection.CacheDir, 0o700)
	}
}

func TestPrepareRejectsSymlinkedCachePath(t *testing.T) {
	for _, pathParts := range [][]string{
		{"aicli"},
		{"aicli", "ozon"},
		{"aicli", "ozon", "contexts"},
		{"aicli", "ozon", "contexts", "seller-a"},
	} {
		t.Run(strings.Join(pathParts, "/"), func(t *testing.T) {
			configHome := t.TempDir()
			cacheHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configHome)
			t.Setenv("XDG_CACHE_HOME", cacheHome)
			parent := cacheHome
			if len(pathParts) > 1 {
				parent = filepath.Join(append([]string{cacheHome}, pathParts[:len(pathParts)-1]...)...)
				if err := os.MkdirAll(parent, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(parent, pathParts[len(pathParts)-1])); err != nil {
				t.Fatal(err)
			}
			manager := New("ozon", "OZON_CONTEXT")
			selection, _, err := manager.ResolveArgs([]string{"ozon", "--context", "seller-a"})
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Prepare(selection); err == nil || !strings.Contains(err.Error(), "not a symlink") {
				t.Fatalf("Prepare error = %v", err)
			}
		})
	}
}

func TestPrepareRejectsInsecureCachePermissions(t *testing.T) {
	if !runtimePermsSupported() {
		t.Skip("POSIX permissions are unavailable")
	}
	for _, pathParts := range [][]string{
		{"aicli"},
		{"aicli", "ozon"},
	} {
		t.Run(strings.Join(pathParts, "/"), func(t *testing.T) {
			configHome := t.TempDir()
			cacheHome := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", configHome)
			t.Setenv("XDG_CACHE_HOME", cacheHome)
			path := filepath.Join(append([]string{cacheHome}, pathParts...)...)
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o777); err != nil {
				t.Fatal(err)
			}
			manager := New("ozon", "OZON_CONTEXT")
			selection, _, err := manager.ResolveArgs([]string{"ozon", "--context", "seller-a"})
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Prepare(selection); err == nil || !strings.Contains(err.Error(), "permissions") && !strings.Contains(err.Error(), "writable") {
				t.Fatalf("Prepare error = %v", err)
			}
		})
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s permissions = %o, want %o", path, got, want)
	}
}

func runtimePermsSupported() bool {
	return os.PathSeparator == '/'
}
