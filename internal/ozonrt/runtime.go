package ozonrt

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
	DefaultBaseURL = "https://api-seller.ozon.ru"
	DefaultSpecURL = "https://raw.githubusercontent.com/PCDCK/ozon-mcp/7e1bea2dd7510064311f1d357d014bb012a18847/src/ozon_mcp/data/seller_swagger.json"
	AuthType       = "ozon-seller-headers"
)

type Config struct {
	BaseURL string
	SpecURL string
}

type Session struct {
	BaseURL          string
	BaseURLSource    string
	Credentials      Credentials
	HasCredentials   bool
	CredentialSource string
}

func LoadSession() (Session, Config, error) {
	return LoadSessionWithContext(contextManager.DefaultSelection())
}

// LoadSessionWithContext reads one selected account context.
func LoadSessionWithContext(selection contextflow.Selection) (Session, Config, error) {
	file, err := LoadFileConfig(ConfigPathFor(selection))
	if err != nil {
		return Session{}, Config{}, err
	}
	env := readEnvironmentSnapshot()
	baseURL, source, err := resolveBaseURL(file, env)
	if err != nil {
		return Session{}, Config{}, err
	}
	session := Session{BaseURL: baseURL, BaseURLSource: source}
	if creds, ok, err := resolveCredentials(file, env); err != nil {
		return Session{}, Config{}, err
	} else if ok {
		session.Credentials = creds
		session.HasCredentials = true
		session.CredentialSource = creds.Source
	}
	cfg := Config{BaseURL: baseURL, SpecURL: firstNonEmpty(env.SpecURL, DefaultSpecURL)}
	if err := validateHTTPURL("OZON_BASE_URL", cfg.BaseURL); err != nil {
		return Session{}, Config{}, err
	}
	if err := validateHTTPURL("OZON_SPEC_URL", cfg.SpecURL); err != nil {
		return Session{}, Config{}, err
	}
	return session, cfg, nil
}

func NewCLIWithSession(cfg Config, session Session, version, commit string) *restish.CLI {
	return NewCLIWithContext(cfg, session, version, commit, contextManager.DefaultSelection())
}

// NewCLIWithContext binds a selected account context to the CLI lifetime.
func NewCLIWithContext(cfg Config, session Session, version, commit string, selection contextflow.Selection) *restish.CLI {
	configureStatePathsFor(selection)
	policy := &SafetyPolicy{}
	cli := restish.New()
	cli.SetCommandName("ozon")
	cli.SetCommandDescription(
		"Ozon Seller API CLI",
		"CLI generated from the complete Ozon Seller OpenAPI description.\n\nLocal commands:\n  auth login|status|logout   manage Client-Id and Api-Key\n  context current|list|use  select an account context\n\nConfiguration:\n  --context selects an account for one invocation.\n  --rsh-config is ignored; use ozon auth or OZON_* environment variables.\n",
	)
	cli.SetVersion(formatVersion(version, commit))
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{
		"ozon": {
			BaseURL:       cfg.BaseURL,
			SpecURL:       cfg.SpecURL,
			CommandLayout: "tags",
			Profiles: map[string]*restish.ProfileConfig{
				"default": {Credentials: map[string]*restishconfig.CredentialConfig{
					securitySchemeName: {Auth: &restish.AuthConfig{Type: AuthType}},
				}},
			},
		},
	}})
	cli.SetCommandSurface(restish.CommandSurface{PromotedAPI: "ozon", SupportCommandNamespace: "cli"})
	cli.AddLoader(SpecLoader{Policy: policy})
	cli.AddAuthHandler(AuthType, &HeaderAuth{Session: session, Policy: policy, StateDir: selection.ConfigDir})
	return cli
}

func RunCLI(cli *restish.CLI, args []string) error {
	return RunCLIWithContext(cli, args, contextManager.DefaultSelection())
}

// RunCLIWithContext executes Restish with context-isolated state.
func RunCLIWithContext(cli *restish.CLI, args []string, selection contextflow.Selection) error {
	if cli == nil {
		return fmt.Errorf("Restish CLI is required")
	}
	cacheDir := os.Getenv("RSH_CACHE_DIR")
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o700); err != nil {
			return fmt.Errorf("initialize Ozon cache: %w", err)
		}
		cachePath := specCachePath(cacheDir, restishengine.ConfigPath(selection.ConfigDir), "ozon")
		if _, err := validateSecureCacheFilePath(cacheDir, cachePath); err != nil {
			return fmt.Errorf("validate Ozon spec cache: %w", err)
		}
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
		if cli.Stderr != nil {
			w = cli.Stderr
		}
		fmt.Fprintln(w, "warning: --rsh-config is ignored; Ozon configuration comes from config.toml / environment variables")
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
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func formatVersion(version, commit string) string {
	version = firstNonEmpty(version, "dev")
	if commit = strings.TrimSpace(commit); commit != "" && commit != "unknown" {
		return version + " (" + commit + ")"
	}
	return version
}
