package ozonrt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	toml "github.com/pelletier/go-toml/v2"
)

const (
	AuthModeKey   = "key"
	configFile    = "config.toml"
	configDirPerm = 0o700
	configPerm    = 0o600
)

type AuthConfig struct {
	Mode     string `toml:"mode"`
	ClientID string `toml:"client_id"`
	APIKey   string `toml:"api_key"`
}

type FileConfig struct {
	BaseURL string      `toml:"base_url,omitempty"`
	Auth    *AuthConfig `toml:"auth,omitempty"`
}

func ConfigDir() string {
	if dir := appStateDir("XDG_CONFIG_HOME", ".config"); dir != "" {
		return filepath.Join(dir, "aicli", "ozon")
	}
	return ""
}

func ConfigPath() string {
	if dir := ConfigDir(); dir != "" {
		return filepath.Join(dir, configFile)
	}
	return ""
}

func LoadFileConfig(path string) (FileConfig, error) {
	if path == "" {
		return FileConfig{}, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, nil
		}
		return FileConfig{}, fmt.Errorf("stat Ozon config: %w", err)
	}
	if err := secureFile(path, info); err != nil {
		return FileConfig{}, err
	}
	if dirInfo, err := os.Lstat(filepath.Dir(path)); err != nil {
		return FileConfig{}, fmt.Errorf("stat Ozon config dir: %w", err)
	} else if err := secureDir(filepath.Dir(path), dirInfo); err != nil {
		return FileConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("read Ozon config: %w", err)
	}
	var file FileConfig
	if len(strings.TrimSpace(string(data))) != 0 {
		if err := toml.Unmarshal(data, &file); err != nil {
			return FileConfig{}, fmt.Errorf("parse Ozon config: %w", err)
		}
	}
	file.BaseURL = strings.TrimSpace(file.BaseURL)
	if file.Auth != nil {
		if err := validateAuth(file.Auth); err != nil {
			return FileConfig{}, err
		}
	}
	return file, nil
}

func SaveLogin(path, baseURL string, auth *AuthConfig) error {
	if path == "" {
		return fmt.Errorf("Ozon config path is unavailable")
	}
	normalized, err := authflow.NormalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	if err := validateAuth(auth); err != nil {
		return err
	}
	file, err := loadNonSecret(path)
	if err != nil {
		return err
	}
	file.BaseURL, file.Auth = normalized, auth
	return writeConfig(path, file)
}

func ClearAuthConfig(path string) error {
	if path == "" {
		return fmt.Errorf("Ozon config path is unavailable")
	}
	file, err := loadNonSecret(path)
	if err != nil {
		return err
	}
	file.Auth = nil
	return writeConfig(path, file)
}

func loadNonSecret(path string) (FileConfig, error) {
	file, err := LoadFileConfig(path)
	if err != nil {
		return FileConfig{}, err
	}
	file.Auth = nil
	return file, nil
}

func validateAuth(auth *AuthConfig) error {
	if auth == nil {
		return fmt.Errorf("auth config is required")
	}
	auth.Mode = strings.TrimSpace(auth.Mode)
	auth.ClientID = strings.TrimSpace(auth.ClientID)
	auth.APIKey = strings.TrimSpace(auth.APIKey)
	if auth.Mode != AuthModeKey {
		return fmt.Errorf("unsupported auth mode %q: expected %q", auth.Mode, AuthModeKey)
	}
	if auth.ClientID == "" || auth.APIKey == "" {
		return fmt.Errorf("key auth requires client_id and api_key")
	}
	return nil
}

func writeConfig(path string, file FileConfig) error {
	dir := filepath.Dir(path)
	if err := ensureSecureDir(dir); err != nil {
		return err
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode Ozon config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create Ozon config temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(configPerm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write Ozon config temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync Ozon config temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close Ozon config temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace Ozon config: %w", err)
	}
	return nil
}

func ensureSecureDir(path string) error {
	if err := os.MkdirAll(path, configDirPerm); err != nil {
		return fmt.Errorf("create Ozon config dir: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat Ozon config dir: %w", err)
	}
	if err := secureDir(path, info); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, configDirPerm)
	}
	return nil
}

func secureDir(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("Ozon config directory must be a directory, not a symlink: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Ozon config directory permissions must be 0700 or stricter: %s", path)
	}
	return nil
}

func secureFile(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("Ozon config must be a regular file, not a symlink: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Ozon config permissions must be 0600 or stricter: %s", path)
	}
	return nil
}
