// Package restishengine isolates branded CLIs from user restish.json config.
package restishengine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	restish "github.com/rest-sh/restish/v2"
	restishconfig "github.com/rest-sh/restish/v2/config"
)

const engineFileName = "restish.json"

var engineEnvironmentMu sync.Mutex

// Isolate pins Restish to an empty engine config under stateDir/engine so
// SetDefaultConfig is the only source of Base URL and auth mapping.
// RSH_CONFIG is overwritten to that engine file. Transient runs also use a
// transient cache directory; persistent runs keep the caller's cache setting.
func Isolate(cli *restish.CLI, stateDir string) (restore func(), err error) {
	if cli == nil {
		return nil, fmt.Errorf("restish CLI is required")
	}
	engineEnvironmentMu.Lock()
	locked := true
	transient := false
	var previousConfig, previousCache string
	var hadPreviousConfig, hadPreviousCache bool
	configPinned := false
	cachePinned := false
	defer func() {
		if err != nil {
			if configPinned {
				restoreEnvironment("RSH_CONFIG", previousConfig, hadPreviousConfig)
			}
			if cachePinned {
				restoreEnvironment("RSH_CACHE_DIR", previousCache, hadPreviousCache)
			}
		}
		if err != nil && transient && stateDir != "" {
			_ = os.RemoveAll(stateDir)
		}
		if locked {
			engineEnvironmentMu.Unlock()
		}
	}()
	stateDirExisted := true
	if stateDir == "" {
		stateDirExisted = false
	} else if _, statErr := os.Lstat(stateDir); os.IsNotExist(statErr) {
		stateDirExisted = false
	}
	if stateDir == "" {
		stateDir, err = os.MkdirTemp("", "aicli-restish-")
		if err != nil {
			return nil, fmt.Errorf("create transient Restish state dir: %w", err)
		}
		transient = true
	} else if err := ensureSecureStateDir(stateDir); err != nil {
		if stateDirExisted {
			return nil, err
		}
		stateDir, err = os.MkdirTemp("", "aicli-restish-")
		if err != nil {
			return nil, fmt.Errorf("create transient Restish state dir after persistent state failure: %w", err)
		}
		transient = true
	}
	var engineFile string
	for {
		engineDir := EngineDir(stateDir)
		prepareErr := ensureSecureEngineDir(engineDir)
		if prepareErr == nil {
			engineFile = ConfigPath(stateDir)
			prepareErr = ensureEmptyEngineConfig(engineFile)
		}
		if prepareErr == nil {
			break
		}
		if transient {
			return nil, prepareErr
		}
		if safetyErr := validateTransientFallback(stateDir); safetyErr != nil {
			return nil, safetyErr
		}
		stateDir, err = os.MkdirTemp("", "aicli-restish-")
		if err != nil {
			return nil, fmt.Errorf("create transient Restish state after persistent engine failure: %w", err)
		}
		transient = true
	}
	previousConfig, hadPreviousConfig = os.LookupEnv("RSH_CONFIG")
	previousCache, hadPreviousCache = os.LookupEnv("RSH_CACHE_DIR")
	if err := os.Setenv("RSH_CONFIG", engineFile); err != nil {
		return nil, fmt.Errorf("pin RSH_CONFIG: %w", err)
	}
	configPinned = true
	if transient {
		if err := os.Setenv("RSH_CACHE_DIR", filepath.Join(stateDir, "cache")); err != nil {
			return nil, fmt.Errorf("pin transient RSH_CACHE_DIR: %w", err)
		}
		cachePinned = true
	}
	cli.Paths = restishconfig.NewPathsWithConfigFile(engineFile)
	var once sync.Once
	locked = false
	return func() {
		once.Do(func() {
			restoreEnvironment("RSH_CONFIG", previousConfig, hadPreviousConfig)
			if transient {
				restoreEnvironment("RSH_CACHE_DIR", previousCache, hadPreviousCache)
				_ = os.RemoveAll(stateDir)
			}
			engineEnvironmentMu.Unlock()
		})
	}, nil
}

func restoreEnvironment(name, value string, existed bool) {
	if existed {
		_ = os.Setenv(name, value)
	} else {
		_ = os.Unsetenv(name)
	}
}

// EngineDir returns the private Restish state directory for a branded CLI.
func EngineDir(stateDir string) string { return filepath.Join(stateDir, "engine") }

// ConfigPath returns the isolated Restish configuration path.
func ConfigPath(stateDir string) string { return filepath.Join(EngineDir(stateDir), engineFileName) }

// TokenCachePath returns the token cache used by the isolated Restish runtime.
func TokenCachePath(stateDir string) string { return filepath.Join(EngineDir(stateDir), "tokens.cbor") }

func ensureSecureStateDir(stateDir string) error {
	info, err := os.Lstat(stateDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat Restish state dir: %w", err)
		}
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return fmt.Errorf("create Restish state dir: %w", err)
		}
		info, err = os.Lstat(stateDir)
		if err != nil {
			return fmt.Errorf("stat Restish state dir: %w", err)
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Restish state dir must not be a symlink: %s", stateDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("Restish state path is not a directory: %s", stateDir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Restish state dir permissions must be 0700 or stricter: %s", stateDir)
	}
	return nil
}

func ensureSecureEngineDir(engineDir string) error {
	info, err := os.Lstat(engineDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat Restish engine dir: %w", err)
		}
		if err := os.MkdirAll(engineDir, 0o700); err != nil {
			return fmt.Errorf("create Restish engine dir: %w", err)
		}
		info, err = os.Lstat(engineDir)
		if err != nil {
			return fmt.Errorf("stat Restish engine dir: %w", err)
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Restish engine dir must not be a symlink: %s", engineDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("Restish engine path is not a directory: %s", engineDir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Restish engine dir permissions must be 0700 or stricter: %s", engineDir)
	}
	if err := os.Chmod(engineDir, 0o700); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("chmod Restish engine dir: %w", err)
	}
	return nil
}

func validateTransientFallback(stateDir string) error {
	engineDir := EngineDir(stateDir)
	info, err := os.Lstat(engineDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat Restish engine dir before transient fallback: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing transient fallback for Restish engine symlink: %s", engineDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing transient fallback for unsafe Restish engine path: %s", engineDir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("refusing transient fallback: Restish engine dir permissions must be 0700 or stricter: %s", engineDir)
	}
	configPath := ConfigPath(stateDir)
	info, err = os.Lstat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat Restish engine config before transient fallback: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing transient fallback for Restish engine config symlink: %s", configPath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing transient fallback for unsafe Restish engine config: %s", configPath)
	}
	return nil
}

// FilterConfigFlags removes --rsh-config / -rsh-config style overrides so a
// branded CLI cannot load a second Base URL/auth mapping from Restish config.
func FilterConfigFlags(args []string) (filtered []string, stripped bool) {
	filtered = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			filtered = append(filtered, args[i:]...)
			break
		}
		switch {
		case arg == "--rsh-config" || arg == "-rsh-config":
			stripped = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		case strings.HasPrefix(arg, "--rsh-config=") || strings.HasPrefix(arg, "-rsh-config="):
			stripped = true
		default:
			filtered = append(filtered, arg)
		}
	}
	return filtered, stripped
}

// ForceNoResponseCache prevents authenticated API responses from being reused
// after credentials change. Spec caching remains available to Restish.
func ForceNoResponseCache(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(args)+1)
	filtered = append(filtered, args[0], "--rsh-no-cache")
	separatorSeen := false
	for _, arg := range args[1:] {
		if arg == "--" {
			separatorSeen = true
			filtered = append(filtered, arg)
			continue
		}
		if !separatorSeen && (arg == "--rsh-no-cache" || strings.HasPrefix(arg, "--rsh-no-cache=")) {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

func ensureEmptyEngineConfig(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat Restish engine config: %w", err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			return fmt.Errorf("write Restish engine config: %w", err)
		}
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Restish engine config must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Restish engine config must be a regular file: %s", path)
	}
	// Always reset to empty so a leftover restish.json cannot override defaults.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".restish.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create Restish engine temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod Restish engine temp: %w", err)
	}
	if _, err := tmp.Write([]byte("{}\n")); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write Restish engine temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close Restish engine temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace Restish engine config: %w", err)
	}
	return nil
}
