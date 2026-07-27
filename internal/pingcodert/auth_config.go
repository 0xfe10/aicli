package pingcodert

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	AuthModeClient = "client"
	AuthModeToken  = "token"

	configFileName = "config.toml"
	dirPerm        = 0o700
	filePerm       = 0o600
	groupOtherBits = 0o077
)

// AuthConfig is the persisted [auth] section.
type AuthConfig struct {
	Mode         string `toml:"mode"`
	ClientID     string `toml:"client_id,omitempty"`
	ClientSecret string `toml:"client_secret,omitempty"`
	AccessToken  string `toml:"access_token,omitempty"`
}

type configFile struct {
	Auth *AuthConfig `toml:"auth,omitempty"`
}

// ConfigDir returns $XDG_CONFIG_HOME/aicli/pingcode or ~/.config/aicli/pingcode.
func ConfigDir() string {
	if dir := appStateDir("XDG_CONFIG_HOME", ".config"); dir != "" {
		return filepath.Join(dir, "aicli", "pingcode")
	}
	return ""
}

// ConfigPath returns the absolute path of config.toml.
func ConfigPath() string {
	dir := ConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, configFileName)
}

// LoadAuthConfig reads and validates the [auth] section from path.
// Missing files yield (nil, nil).
func LoadAuthConfig(path string) (*AuthConfig, error) {
	if path == "" {
		return nil, fmt.Errorf("PingCode config path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat PingCode config: %w", err)
	}
	if err := rejectInsecureFile(path, info); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read PingCode config: %w", err)
	}
	var file configFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse PingCode config: %w", err)
	}
	if file.Auth == nil {
		return nil, nil
	}
	if err := validateAuthConfig(file.Auth); err != nil {
		return nil, err
	}
	return file.Auth, nil
}

// SaveAuthConfig atomically writes auth into config.toml with 0600 permissions.
func SaveAuthConfig(path string, auth *AuthConfig) error {
	if path == "" {
		return fmt.Errorf("PingCode config path is unavailable")
	}
	if err := validateAuthConfig(auth); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := ensureSecureDir(dir); err != nil {
		return err
	}
	file, err := loadConfigFile(path)
	if err != nil {
		return err
	}
	file.Auth = auth
	return writeConfigFileAtomic(path, file)
}

// ClearAuthConfig removes the [auth] section while preserving other keys.
func ClearAuthConfig(path string) error {
	if path == "" {
		return fmt.Errorf("PingCode config path is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat PingCode config: %w", err)
	}
	if err := rejectInsecureFile(path, info); err != nil {
		return err
	}
	file, err := loadConfigFile(path)
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
	case AuthModeClient:
		if strings.TrimSpace(auth.ClientID) == "" || strings.TrimSpace(auth.ClientSecret) == "" {
			return fmt.Errorf("client auth requires client_id and client_secret")
		}
		if strings.TrimSpace(auth.AccessToken) != "" {
			return fmt.Errorf("client auth must not set access_token")
		}
	case AuthModeToken:
		if strings.TrimSpace(auth.AccessToken) == "" {
			return fmt.Errorf("token auth requires access_token")
		}
		if strings.TrimSpace(auth.ClientID) != "" || strings.TrimSpace(auth.ClientSecret) != "" {
			return fmt.Errorf("token auth must not set client_id or client_secret")
		}
	default:
		return fmt.Errorf("unsupported auth mode %q: expected %q or %q", auth.Mode, AuthModeClient, AuthModeToken)
	}
	auth.Mode = mode
	auth.ClientID = strings.TrimSpace(auth.ClientID)
	auth.ClientSecret = strings.TrimSpace(auth.ClientSecret)
	auth.AccessToken = strings.TrimSpace(auth.AccessToken)
	return nil
}

func loadConfigFile(path string) (configFile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configFile{}, nil
		}
		return configFile{}, fmt.Errorf("stat PingCode config: %w", err)
	}
	if err := rejectInsecureFile(path, info); err != nil {
		return configFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return configFile{}, fmt.Errorf("read PingCode config: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return configFile{}, nil
	}
	var file configFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return configFile{}, fmt.Errorf("parse PingCode config: %w", err)
	}
	return file, nil
}

func writeConfigFileAtomic(path string, file configFile) error {
	dir := filepath.Dir(path)
	if err := ensureSecureDir(dir); err != nil {
		return err
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode PingCode config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create PingCode config temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod PingCode config temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write PingCode config temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync PingCode config temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close PingCode config temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace PingCode config: %w", err)
	}
	return nil
}

func ensureSecureDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat PingCode config dir: %w", err)
		}
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create PingCode config dir: %w", err)
		}
		info, err = os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("stat PingCode config dir: %w", err)
		}
	}
	if err := rejectInsecureDir(dir, info); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, dirPerm); err != nil {
			return fmt.Errorf("chmod PingCode config dir: %w", err)
		}
	}
	return nil
}

func rejectInsecureDir(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("PingCode config directory must not be a symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("PingCode config path is not a directory: %s", path)
	}
	if permissionTooOpen(info.Mode(), groupOtherBits) {
		return fmt.Errorf("PingCode config directory permissions must be 0700 or stricter: %s", path)
	}
	return nil
}

func rejectInsecureFile(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("PingCode config must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("PingCode config must be a regular file: %s", path)
	}
	if permissionTooOpen(info.Mode(), groupOtherBits) {
		return fmt.Errorf("PingCode config permissions must be 0600 or stricter: %s", path)
	}
	return nil
}

func permissionTooOpen(mode os.FileMode, mask os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return mode.Perm()&mask != 0
}
