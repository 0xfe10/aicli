package fnsrt

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/restishengine"
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
	PlaceholderHost  = "fns.example.com"
)

// Config bootstraps the embedded Restish runtime for FNS.
type Config struct {
	BaseURL string
	SpecURL string
	Client  string
	Version string
}

// Session is a single-run snapshot of Base URL and credentials.
type Session struct {
	BaseURL          string
	BaseURLSource    string
	Credentials      Credentials
	HasCredentials   bool
	CredentialSource string
}

// LoadSession reads Base URL and credentials once for this CLI process.
func LoadSession(version string) (Session, Config, error) {
	session, file, env, err := loadSessionSnapshot()
	if err != nil {
		return Session{}, Config{}, err
	}
	cfg, err := configFromSnapshot(session, file, env, version)
	if err != nil {
		return Session{}, Config{}, err
	}
	return session, cfg, nil
}

func loadSessionSnapshot() (Session, FileConfig, environmentSnapshot, error) {
	path := ConfigPath()
	var file FileConfig
	if path != "" {
		loaded, err := LoadFileConfig(path)
		if err != nil {
			return Session{}, FileConfig{}, environmentSnapshot{}, err
		}
		file = loaded
	}
	env := readEnvironmentSnapshot()
	baseURL, baseSource, err := baseURLFromSnapshot(file, env)
	if err != nil {
		return Session{}, FileConfig{}, environmentSnapshot{}, err
	}
	session := Session{BaseURL: baseURL, BaseURLSource: baseSource}
	if creds, configured := credentialsFromSnapshot(file, env); configured {
		session.Credentials = creds
		session.HasCredentials = true
		session.CredentialSource = creds.Source
	}
	return session, file, env, nil
}

// LoadConfig resolves base URL, spec URL, and client identity.
func LoadConfig(version string) (Config, error) {
	_, cfg, err := LoadSession(version)
	return cfg, err
}

func configFromSnapshot(session Session, file FileConfig, env environmentSnapshot, version string) (Config, error) {
	cfg := Config{
		BaseURL: session.BaseURL,
		SpecURL: firstNonEmpty(env.SpecURL, DefaultSpecURL),
		Client:  firstNonEmpty(env.Client, file.Client, DefaultClient),
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

// NewCLIWithSession binds Base URL and credentials for the lifetime of the CLI.
func NewCLIWithSession(cfg Config, session Session, version, commit string) *restish.CLI {
	if session.BaseURL == "" {
		session.BaseURL = cfg.BaseURL
	}
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
	cli.SetCommandDescription(
		"Fast Note Sync CLI",
		"CLI generated from the FNS OpenAPI description.\n\nLocal commands:\n  auth login|status|logout   manage Base URL and credentials\n\nConfiguration:\n  --rsh-config is ignored; use fns auth or FNS_* environment variables.\n",
	)
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
	cli.AddAuthHandler(AuthType, &BearerAuth{Session: session})
	return cli
}

// RunCLI executes Restish after stripping Restish config overrides.
func RunCLI(cli *restish.CLI, args []string) error {
	if cli == nil {
		return fmt.Errorf("Restish CLI is required")
	}
	restore, err := restishengine.Isolate(cli, ConfigDir())
	if err != nil {
		return fmt.Errorf("initialize isolated Restish runtime: %w", err)
	}
	defer restore()
	filtered, stripped := restishengine.FilterConfigFlags(args)
	filtered = restishengine.ForceNoResponseCache(filtered)
	if stripped {
		w := io.Writer(os.Stderr)
		if cli != nil && cli.Stderr != nil {
			w = cli.Stderr
		}
		fmt.Fprintln(w, `warning: --rsh-config is ignored; FNS Base URL and credentials come from config.toml / environment variables (fns auth login)`)
	}
	return cli.Run(filtered)
}

func configureStatePaths() {
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
	host := authflow.CanonicalHostname(u.Hostname())
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
