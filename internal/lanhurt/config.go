package lanhurt

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

const (
	DefaultBaseURL = "https://lanhuapp.com"
	AuthModeCookie = "cookie"
	configFile     = "config.toml"
)

var contextManager = contextflow.New("lanhu", "LANHU_CONTEXT")

type AuthConfig struct {
	Mode      string `toml:"mode"`
	Cookie    string `toml:"cookie"`
	DDSCookie string `toml:"dds_cookie,omitempty"`
}

type FileConfig struct {
	Auth *AuthConfig `toml:"auth,omitempty"`
}

type Session struct {
	Cookie           string
	DDSCookie        string
	HasCredentials   bool
	CredentialSource string
}

func ContextManager() contextflow.Manager { return contextManager }

func ConfigPathFor(selection contextflow.Selection) string {
	if selection.ConfigDir == "" {
		return ""
	}
	return filepath.Join(selection.ConfigDir, configFile)
}

func LoadSessionWithContext(selection contextflow.Selection) (Session, error) {
	file, err := loadFileConfig(ConfigPathFor(selection))
	if err != nil {
		return Session{}, err
	}
	cookie, dds, source := "", "", ""
	if file.Auth != nil {
		cookie, dds, source = file.Auth.Cookie, file.Auth.DDSCookie, authflow.SourceConfig
	}
	if value := strings.TrimSpace(os.Getenv("LANHU_COOKIE")); value != "" {
		cookie, source = value, authflow.SourceEnvironment
	}
	if value := strings.TrimSpace(os.Getenv("DDS_COOKIE")); value != "" {
		dds, source = value, authflow.SourceEnvironment
	}
	if dds == "" {
		dds = cookie
	}
	return Session{Cookie: cookie, DDSCookie: dds, HasCredentials: cookie != "", CredentialSource: source}, nil
}

func loadFileConfig(path string) (FileConfig, error) {
	if path == "" {
		return FileConfig{}, nil
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return FileConfig{}, nil
	}
	if err != nil {
		return FileConfig{}, fmt.Errorf("stat Lanhu config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return FileConfig{}, fmt.Errorf("Lanhu config must be a regular file, not a symlink: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return FileConfig{}, fmt.Errorf("Lanhu config permissions must be 0600 or stricter: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, err
	}
	var file FileConfig
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := toml.Unmarshal(data, &file); err != nil {
			return FileConfig{}, fmt.Errorf("parse Lanhu config: %w", err)
		}
	}
	if file.Auth != nil && (file.Auth.Mode != AuthModeCookie || strings.TrimSpace(file.Auth.Cookie) == "") {
		return FileConfig{}, fmt.Errorf("invalid Lanhu cookie auth config")
	}
	return file, nil
}

func saveLogin(path, cookie, ddsCookie string) error {
	if path == "" || strings.TrimSpace(cookie) == "" {
		return fmt.Errorf("Lanhu config path and cookie are required")
	}
	return writeConfig(path, FileConfig{Auth: &AuthConfig{Mode: AuthModeCookie, Cookie: strings.TrimSpace(cookie), DDSCookie: strings.TrimSpace(ddsCookie)}})
}

func clearLogin(path string) error { return writeConfig(path, FileConfig{}) }

func writeConfig(path string, file FileConfig) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Lanhu config must not be a symlink: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func EnvironmentAuthPresent() bool {
	return strings.TrimSpace(os.Getenv("LANHU_COOKIE")) != "" || strings.TrimSpace(os.Getenv("DDS_COOKIE")) != ""
}
