package paseobridgert

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

var contextManager = contextflow.New("paseo-bridge", "PASEO_BRIDGE_CONTEXT")

// ContextManager returns PASEO_BRIDGE's account-context manager.
func ContextManager() contextflow.Manager { return contextManager }

const (
	AuthModeToken = "token"

	configFileName = "config.toml"
	dirPerm        = 0o700
	filePerm       = 0o600
	groupOtherBits = 0o077
)

// AuthConfig is the persisted [auth] section.
type AuthConfig struct {
	Mode        string `toml:"mode"`
	AccessToken string `toml:"access_token,omitempty"`
}

// FileConfig is the persisted PASEO_BRIDGE configuration file.
type FileConfig struct {
	BaseURL string      `toml:"base_url,omitempty"`
	Auth    *AuthConfig `toml:"auth,omitempty"`
}

// ConfigDir returns $XDG_CONFIG_HOME/aicli/paseo-bridge or ~/.config/aicli/paseo-bridge.
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
		return FileConfig{}, fmt.Errorf("PASEO_BRIDGE config path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, nil
		}
		return FileConfig{}, fmt.Errorf("stat PASEO_BRIDGE config: %w", err)
	}
	if err := rejectInsecureFile(path, info); err != nil {
		return FileConfig{}, err
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return FileConfig{}, fmt.Errorf("stat PASEO_BRIDGE config dir: %w", err)
	}
	if err := rejectInsecureDir(filepath.Dir(path), dirInfo); err != nil {
		return FileConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("read PASEO_BRIDGE config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return FileConfig{}, nil
	}
	var file FileConfig
	if err := toml.Unmarshal(data, &file); err != nil {
		return FileConfig{}, fmt.Errorf("parse PASEO_BRIDGE config: %w", err)
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
		return fmt.Errorf("PASEO_BRIDGE config path is unavailable")
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

// ClearAuthConfig removes the [auth] section while preserving base_url.
func ClearAuthConfig(path string) error {
	if path == "" {
		return fmt.Errorf("PASEO_BRIDGE config path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat PASEO_BRIDGE config: %w", err)
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
	case AuthModeToken:
		if strings.TrimSpace(auth.AccessToken) == "" {
			return fmt.Errorf("token auth requires access_token")
		}
	default:
		return fmt.Errorf("unsupported auth mode %q: expected %q", auth.Mode, AuthModeToken)
	}
	auth.Mode = mode
	auth.AccessToken = strings.TrimSpace(auth.AccessToken)
	return nil
}

func writeConfigFileAtomic(path string, file FileConfig) error {
	dir := filepath.Dir(path)
	if err := ensureSecureDir(dir); err != nil {
		return err
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode PASEO_BRIDGE config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create PASEO_BRIDGE config temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod PASEO_BRIDGE config temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write PASEO_BRIDGE config temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync PASEO_BRIDGE config temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close PASEO_BRIDGE config temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace PASEO_BRIDGE config: %w", err)
	}
	return nil
}

func ensureSecureDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat PASEO_BRIDGE config dir: %w", err)
		}
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create PASEO_BRIDGE config dir: %w", err)
		}
		info, err = os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("stat PASEO_BRIDGE config dir: %w", err)
		}
	}
	if err := rejectInsecureDir(dir, info); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, dirPerm); err != nil {
			return fmt.Errorf("chmod PASEO_BRIDGE config dir: %w", err)
		}
	}
	return nil
}

func rejectInsecureDir(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("PASEO_BRIDGE config directory must not be a symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("PASEO_BRIDGE config path is not a directory: %s", path)
	}
	if permissionTooOpen(info.Mode(), groupOtherBits) {
		return fmt.Errorf("PASEO_BRIDGE config directory permissions must be 0700 or stricter: %s", path)
	}
	return nil
}

func rejectInsecureFile(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("PASEO_BRIDGE config must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("PASEO_BRIDGE config must be a regular file: %s", path)
	}
	if permissionTooOpen(info.Mode(), groupOtherBits) {
		return fmt.Errorf("PASEO_BRIDGE config permissions must be 0600 or stricter: %s", path)
	}
	return nil
}

func permissionTooOpen(mode os.FileMode, mask os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return mode.Perm()&mask != 0
}
