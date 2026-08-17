package pingcodert

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/restishengine"
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

// Session is a single-run snapshot of Base URL and credentials.
type Session struct {
	BaseURL          string
	BaseURLSource    string
	SpecURL          string
	Credentials      Credentials
	HasCredentials   bool
	CredentialSource string
}

// LoadSession reads Base URL and credentials once for this CLI process.
func LoadSession() (Session, error) {
	return LoadSessionWithContext(contextManager.DefaultSelection())
}

// LoadSessionWithContext reads one selected account context.
func LoadSessionWithContext(selection contextflow.Selection) (Session, error) {
	session, _, _, err := loadSessionSnapshotWithContext(selection)
	return session, err
}

func loadSessionSnapshot() (Session, FileConfig, environmentSnapshot, error) {
	return loadSessionSnapshotWithContext(contextManager.DefaultSelection())
}

func loadSessionSnapshotWithContext(selection contextflow.Selection) (Session, FileConfig, environmentSnapshot, error) {
	path := ConfigPathFor(selection)
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
	session := Session{BaseURL: baseURL, BaseURLSource: baseSource, SpecURL: firstNonEmpty(env.SpecURL, DefaultSpecURL)}
	creds, configured, err := credentialsFromSnapshot(file, env)
	if err != nil {
		return Session{}, FileConfig{}, environmentSnapshot{}, err
	}
	if !configured {
		return session, file, env, nil
	}
	session.Credentials = creds
	session.HasCredentials = true
	session.CredentialSource = creds.Source
	return session, file, env, nil
}

func LoadConfig() (Config, error) {
	session, err := LoadSession()
	if err != nil {
		return Config{}, err
	}
	return ConfigFromSession(session)
}

// ConfigFromSession builds runtime config from an already-loaded session.
func ConfigFromSession(session Session) (Config, error) {
	cfg := Config{
		APIBaseURL: session.BaseURL,
		SpecURL:    firstNonEmpty(session.SpecURL, DefaultSpecURL),
	}
	if err := validateHTTPURL("PINGCODE_API_BASE_URL", cfg.APIBaseURL); err != nil {
		return Config{}, err
	}
	if err := validateHTTPURL("PINGCODE_SPEC_URL", cfg.SpecURL); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// NewCLIWithSession binds Base URL and credentials for the lifetime of the CLI.
func NewCLIWithSession(cfg Config, session Session, version, commit string) *restish.CLI {
	return NewCLIWithContext(cfg, session, version, commit, contextManager.DefaultSelection())
}

// NewCLIWithContext binds a selected account context to the CLI lifetime.
func NewCLIWithContext(cfg Config, session Session, version, commit string, selection contextflow.Selection) *restish.CLI {
	if session.BaseURL == "" {
		session.BaseURL = cfg.APIBaseURL
	}
	configureStatePathsFor(selection)
	cli := restish.New()
	cli.SetCommandName("pingcode")
	cli.SetCommandDescription(
		"PingCode API CLI",
		"CLI generated from the official PingCode API description.\n\nLocal commands:\n  auth login|status|logout   manage Base URL and credentials\n  context current|list|use  select an account context\n\nConfiguration:\n  --context selects an account for one invocation.\n  --rsh-config is ignored; use pingcode auth or PINGCODE_* environment variables.\n",
	)
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
	cli.AddAuthHandler(AuthType, &ClientCredentialsAuth{Session: session})
	return cli
}

// RunCLI executes Restish after stripping Restish config overrides.
func RunCLI(cli *restish.CLI, args []string) error {
	return RunCLIWithContext(cli, args, contextManager.DefaultSelection())
}

// RunCLIWithContext executes Restish with context-isolated state.
func RunCLIWithContext(cli *restish.CLI, args []string, selection contextflow.Selection) error {
	if cli == nil {
		return fmt.Errorf("Restish CLI is required")
	}
	restore, err := restishengine.Isolate(cli, selection.ConfigDir)
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
		fmt.Fprintln(w, `warning: --rsh-config is ignored; PingCode Base URL and credentials come from config.toml / environment variables (pingcode auth login)`)
	}
	return cli.Run(filtered)
}

func configureStatePaths() {
	configureStatePathsFor(contextManager.DefaultSelection())
}

func configureStatePathsFor(selection contextflow.Selection) {
	if selection.Name != contextflow.DefaultName && selection.CacheDir != "" {
		_ = os.Setenv("RSH_CACHE_DIR", selection.CacheDir)
	} else if os.Getenv("RSH_CACHE_DIR") == "" {
		if selection.CacheDir != "" {
			_ = os.Setenv("RSH_CACHE_DIR", selection.CacheDir)
		}
	}
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
