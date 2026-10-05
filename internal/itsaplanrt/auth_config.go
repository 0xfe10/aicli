package itsaplanrt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/contextflow"
	toml "github.com/pelletier/go-toml/v2"
)

var contextManager = contextflow.New("itsaplan", "ITSAPLAN_CONTEXT")

// ContextManager returns ITSAPLAN's account-context manager.
func ContextManager() contextflow.Manager { return contextManager }

const (
	AuthModeKey = "key"

	configFileName = "config.toml"
	dirPerm        = 0o700
	filePerm       = 0o600
	groupOtherBits = 0o077
)

// AuthConfig is the persisted [auth] section.
type AuthConfig struct {
	Mode   string `toml:"mode"`
	APIKey string `toml:"api_key,omitempty"`
}

// FileConfig is the persisted ITSAPLAN configuration file.
type FileConfig struct {
	BaseURL string      `toml:"base_url,omitempty"`
	Auth    *AuthConfig `toml:"auth,omitempty"`
}

// ConfigDir returns $XDG_CONFIG_HOME/aicli/itsaplan or ~/.config/aicli/itsaplan.
func ConfigDir() string {
	return contextManager.DefaultSelection().ConfigDir
}

// ConfigPath returns the absolute path of config.toml.
func ConfigPath() string {
	return ConfigPathFor(contextManager.DefaultSelection())
}

// ConfigPathFor returns the config path for selection.
func ConfigPathFor(selection contextflow.Selection) string {
	dir := selection.ConfigDir
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, configFileName)
}

// LoadFileConfig reads config.toml. Missing files yield an empty config.
func LoadFileConfig(path string) (FileConfig, error) {
	return loadFileConfig(path, false)
}

func loadFileConfig(path string, discardAuth bool) (FileConfig, error) {
	if path == "" {
		return FileConfig{}, fmt.Errorf("ITSAPLAN config path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, nil
		}
		return FileConfig{}, fmt.Errorf("stat ITSAPLAN config: %w", err)
	}
	if err := rejectInsecureFile(path, info); err != nil {
		return FileConfig{}, err
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return FileConfig{}, fmt.Errorf("stat ITSAPLAN config dir: %w", err)
	}
	if err := rejectInsecureDir(filepath.Dir(path), dirInfo); err != nil {
		return FileConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("read ITSAPLAN config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return FileConfig{}, nil
	}
	var file FileConfig
	if err := toml.Unmarshal(data, &file); err != nil {
		return FileConfig{}, fmt.Errorf("parse ITSAPLAN config: %w", err)
	}
	file.BaseURL = strings.TrimSpace(file.BaseURL)
	if discardAuth {
		file.Auth = nil
		return file, nil
	}
	if file.Auth != nil {
		if err := validateAuthConfig(file.Auth); err != nil {
			return FileConfig{}, err
		}
	}
	return file, nil
}

// SaveLogin atomically writes base_url and [auth] into config.toml.
func SaveLogin(path, baseURL string, auth *AuthConfig) error {
	if path == "" {
		return fmt.Errorf("ITSAPLAN config path is unavailable")
	}
	normalized, err := authflow.NormalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	if err := validateOrigin(normalized); err != nil {
		return err
	}
	if IsPlaceholderBaseURL(normalized) {
		return fmt.Errorf("Base URL must not use the placeholder host %q", PlaceholderHost)
	}
	if err := validateAuthConfig(auth); err != nil {
		return err
	}
	if err := ensureSecureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := loadFileConfig(path, true)
	if err != nil {
		return err
	}
	file.BaseURL = normalized
	file.Auth = auth
	return writeConfigFileAtomic(path, file)
}

// ClearAuthConfig removes the [auth] section while preserving base_url and client.
func ClearAuthConfig(path string) error {
	if path == "" {
		return fmt.Errorf("ITSAPLAN config path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat ITSAPLAN config: %w", err)
	}
	if err := rejectInsecureFile(path, info); err != nil {
		return err
	}
	file, err := loadFileConfig(path, true)
	if err != nil {
		return err
	}
	file.Auth = nil
	return writeConfigFileAtomic(path, file)
}

func validateAuthConfig(auth *AuthConfig) error {
	if auth == nil {
		return fmt.Errorf("auth config is required")
	}
	mode := strings.TrimSpace(auth.Mode)
	switch mode {
	case AuthModeKey:
		if strings.TrimSpace(auth.APIKey) == "" {
			return fmt.Errorf("key auth requires api_key")
		}
	default:
		return fmt.Errorf("unsupported auth mode %q: expected %q", auth.Mode, AuthModeKey)
	}
	auth.Mode = mode
	auth.APIKey = strings.TrimSpace(auth.APIKey)
	return nil
}

func writeConfigFileAtomic(path string, file FileConfig) error {
	dir := filepath.Dir(path)
	if err := ensureSecureDir(dir); err != nil {
		return err
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode ITSAPLAN config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create ITSAPLAN config temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod ITSAPLAN config temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write ITSAPLAN config temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync ITSAPLAN config temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close ITSAPLAN config temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace ITSAPLAN config: %w", err)
	}
	return nil
}

func ensureSecureDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat ITSAPLAN config dir: %w", err)
		}
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create ITSAPLAN config dir: %w", err)
		}
		info, err = os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("stat ITSAPLAN config dir: %w", err)
		}
	}
	if err := rejectInsecureDir(dir, info); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, dirPerm); err != nil {
			return fmt.Errorf("chmod ITSAPLAN config dir: %w", err)
		}
	}
	return nil
}

func rejectInsecureDir(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ITSAPLAN config directory must not be a symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("ITSAPLAN config path is not a directory: %s", path)
	}
	if permissionTooOpen(info.Mode(), groupOtherBits) {
		return fmt.Errorf("ITSAPLAN config directory permissions must be 0700 or stricter: %s", path)
	}
	return nil
}

func rejectInsecureFile(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ITSAPLAN config must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("ITSAPLAN config must be a regular file: %s", path)
	}
	if permissionTooOpen(info.Mode(), groupOtherBits) {
		return fmt.Errorf("ITSAPLAN config permissions must be 0600 or stricter: %s", path)
	}
	return nil
}

func permissionTooOpen(mode os.FileMode, mask os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return mode.Perm()&mask != 0
}
