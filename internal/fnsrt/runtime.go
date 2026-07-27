package fnsrt

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	restish "github.com/rest-sh/restish/v2"
	restishconfig "github.com/rest-sh/restish/v2/config"
)

const (
	DefaultBaseURL = "https://fns.example.com"
	// Pinned to an immutable commit until FNS publishes a stable OpenAPI
	// endpoint (M9). Do not point at mutable tags or branch tips.
	PinnedSpecCommit = "b6b4566352f39e0404530ed1b58248a815a6d763"
	PinnedSpecSHA256 = "ae6a880bb9accf472f45d41a922db67617755ce6b7352aef971e7f969ad0d113"
	DefaultSpecURL   = "https://raw.githubusercontent.com/haierkeys/fast-note-sync-service/" + PinnedSpecCommit + "/docs/swagger.yaml"
	DefaultClient    = "aicli"
	AuthType         = "fns-bearer"
)

// Config bootstraps the embedded Restish runtime for FNS.
type Config struct {
	BaseURL string
	SpecURL string
	Client  string
	Version string
}

// LoadConfig resolves base URL, spec URL, and client identity.
// Environment variables override values from config.toml.
func LoadConfig(version string) (Config, error) {
	var file FileConfig
	if path := ConfigPath(); path != "" {
		loaded, err := LoadFileConfig(path)
		if err != nil {
			return Config{}, err
		}
		file = loaded
	}
	cfg := Config{
		BaseURL: firstNonEmpty(os.Getenv("FNS_BASE_URL"), file.BaseURL, DefaultBaseURL),
		SpecURL: firstNonEmpty(os.Getenv("FNS_SPEC_URL"), DefaultSpecURL),
		Client:  firstNonEmpty(os.Getenv("FNS_CLIENT"), file.Client, DefaultClient),
		Version: strings.TrimSpace(version),
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if err := validateHTTPURL("FNS_BASE_URL", cfg.BaseURL); err != nil {
		return Config{}, err
	}
	if err := validateHTTPURL("FNS_SPEC_URL", cfg.SpecURL); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// NewCLI returns a branded Restish CLI for Fast Note Sync.
func NewCLI(cfg Config, version, commit string) *restish.CLI {
	configureStatePaths()
	if cfg.Client == "" {
		cfg.Client = DefaultClient
	}
	if cfg.Version == "" {
		cfg.Version = strings.TrimSpace(version)
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	cli := restish.New()
	cli.SetCommandName("fns")
	cli.SetCommandDescription("Fast Note Sync CLI", "CLI generated from the FNS OpenAPI description.")
	cli.SetVersion(formatVersion(version, commit))
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{
		"fns": {
			BaseURL:       cfg.BaseURL,
			SpecURL:       cfg.SpecURL,
			CommandLayout: "tags",
			Profiles: map[string]*restish.ProfileConfig{
				"default": {
					Headers: []string{
						"X-Client: " + cfg.Client,
						"X-Client-Name: " + cfg.Client,
						"X-Client-Version: " + cfg.Version,
					},
					Credentials: map[string]*restishconfig.CredentialConfig{
						securitySchemeName: {Auth: &restish.AuthConfig{Type: AuthType}},
					},
				},
			},
		},
	}})
	cli.SetCommandSurface(restish.CommandSurface{
		PromotedAPI:             "fns",
		SupportCommandNamespace: "cli",
	})
	cli.AddLoader(SpecLoader{})
	cli.AddAuthHandler(AuthType, &BearerAuth{})
	return cli
}

func configureStatePaths() {
	if os.Getenv("RSH_CONFIG") == "" && os.Getenv("RSH_CONFIG_DIR") == "" {
		if dir := appStateDir("XDG_CONFIG_HOME", ".config"); dir != "" {
			_ = os.Setenv("RSH_CONFIG_DIR", filepath.Join(dir, "aicli", "fns"))
		}
	}
	if os.Getenv("RSH_CACHE_DIR") == "" {
		if dir := appStateDir("XDG_CACHE_HOME", ".cache"); dir != "" {
			_ = os.Setenv("RSH_CACHE_DIR", filepath.Join(dir, "aicli", "fns"))
		}
	}
}

func appStateDir(envName, homeSuffix string) string {
	if dir := strings.TrimSpace(os.Getenv(envName)); dir != "" && filepath.IsAbs(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, homeSuffix)
}

func validateHTTPURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute HTTP URL", name)
	}
	host := strings.ToLower(u.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return fmt.Errorf("%s must use HTTPS (HTTP is allowed for localhost)", name)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatVersion(version, commit string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "dev"
	}
	if commit = strings.TrimSpace(commit); commit != "" && commit != "unknown" {
		return version + " (" + commit + ")"
	}
	return version
}
