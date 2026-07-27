package pingcodert

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
	DefaultAPIBaseURL = "https://open.pingcode.com"
	DefaultSpecURL    = "https://open.pingcode.com/api_data.json"
	AuthType          = "pingcode-client-credentials"
)

// Config contains the small amount of service-specific configuration needed
// to bootstrap the embedded Restish runtime.
type Config struct {
	APIBaseURL string
	SpecURL    string
}

func LoadConfig() (Config, error) {
	cfg := Config{
		APIBaseURL: envOr("PINGCODE_API_BASE_URL", DefaultAPIBaseURL),
		SpecURL:    envOr("PINGCODE_SPEC_URL", DefaultSpecURL),
	}
	if err := validateHTTPURL("PINGCODE_API_BASE_URL", cfg.APIBaseURL); err != nil {
		return Config{}, err
	}
	if err := validateHTTPURL("PINGCODE_SPEC_URL", cfg.SpecURL); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// NewCLI returns a branded, single-API Restish CLI. Restish owns command
// generation, request execution, retries, pagination, filtering, and output.
func NewCLI(cfg Config, version, commit string) *restish.CLI {
	configureStatePaths()

	cli := restish.New()
	cli.SetCommandName("pingcode")
	cli.SetCommandDescription("PingCode API CLI", "CLI generated from the official PingCode API description.")
	cli.SetVersion(formatVersion(version, commit))
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{
		"pingcode": {
			BaseURL:       cfg.APIBaseURL,
			SpecURL:       cfg.SpecURL,
			CommandLayout: "tags",
			Profiles: map[string]*restish.ProfileConfig{
				"default": {
					Credentials: map[string]*restishconfig.CredentialConfig{
						"enterpriseToken": {Auth: &restish.AuthConfig{Type: AuthType}},
						"userToken":       {Auth: &restish.AuthConfig{Type: AuthType}},
					},
				},
			},
		},
	}})
	cli.SetCommandSurface(restish.CommandSurface{
		PromotedAPI:             "pingcode",
		SupportCommandNamespace: "cli",
	})
	cli.AddLoader(APIDocLoader{})
	cli.AddAuthHandler(AuthType, &ClientCredentialsAuth{})
	return cli
}

func configureStatePaths() {
	if os.Getenv("RSH_CONFIG") == "" && os.Getenv("RSH_CONFIG_DIR") == "" {
		if dir := appStateDir("XDG_CONFIG_HOME", ".config"); dir != "" {
			_ = os.Setenv("RSH_CONFIG_DIR", filepath.Join(dir, "aicli", "pingcode"))
		}
	}
	if os.Getenv("RSH_CACHE_DIR") == "" {
		if dir := appStateDir("XDG_CACHE_HOME", ".cache"); dir != "" {
			_ = os.Setenv("RSH_CACHE_DIR", filepath.Join(dir, "aicli", "pingcode"))
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

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
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
