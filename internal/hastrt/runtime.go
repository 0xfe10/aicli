// Package hastrt binds the hast OpenAPI to the embedded Restish runtime.
package hastrt

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/restishengine"
	restish "github.com/rest-sh/restish/v2"
	restishconfig "github.com/rest-sh/restish/v2/config"
)

const (
	DefaultBaseURL     = "https://backend.hast.so"
	DefaultSpecURL     = "https://hast.so/openapi/task-agent.json"
	PlaceholderHost    = "hast.example.com"
	AuthType           = "hast-key"
	securitySchemeName = "PublicBearer"
)

type Config struct{ BaseURL, SpecURL string }
type Session struct {
	BaseURL, BaseURLSource string
	Credentials            Credentials
	HasCredentials         bool
	CredentialSource       string
}

func LoadSessionWithContext(selection contextflow.Selection) (Session, Config, error) {
	var file FileConfig
	var err error
	if path := ConfigPathFor(selection); path != "" {
		file, err = LoadFileConfig(path)
	}
	if err != nil {
		return Session{}, Config{}, err
	}
	env := readEnvironmentSnapshot()
	base, source, err := baseURLFromSnapshot(file, env)
	if err != nil {
		return Session{}, Config{}, err
	}
	// Strict origin isolation: same-origin redirects cannot enforce subpath scope.
	if err := validateOrigin(base); err != nil {
		return Session{}, Config{}, err
	}
	session := Session{BaseURL: base, BaseURLSource: source}
	if creds, ok := credentialsFromSnapshot(file, env); ok {
		session.Credentials = creds
		session.HasCredentials = true
		session.CredentialSource = creds.Source
	}
	spec := env.SpecURL
	if spec == "" {
		spec = DefaultSpecURL
	}
	spec, err = authflow.NormalizeBaseURL(spec)
	if err != nil {
		return Session{}, Config{}, fmt.Errorf("HAST_SPEC_URL: %w", err)
	}
	return session, Config{BaseURL: base, SpecURL: spec}, nil
}

func NewCLIWithContext(cfg Config, session Session, version, commit string, selection contextflow.Selection) *restish.CLI {
	cli := restish.New()
	cli.SetCommandName("hast")
	cli.SetCommandDescription("Hast Task Agent API CLI", "Commands generated from the selected instance's OpenAPI.\nWrites are never automatically retried. HTTP 202 accepts a task; inspect run events for completion.\nLocal commands: auth login|status|logout; context current|list|use.\nHAST_WRITE_MODE: readonly (default), write, destructive.\n--rsh-config is ignored; use config.toml or HAST_* variables.")
	cli.SetVersion(version + " (" + commit + ")")
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{"hast": {
		BaseURL: cfg.BaseURL, SpecURL: cfg.SpecURL, CommandLayout: "tags",
		Profiles: map[string]*restish.ProfileConfig{"default": {Credentials: map[string]*restishconfig.CredentialConfig{securitySchemeName: {Auth: &restish.AuthConfig{Type: AuthType}}}}},
	}}})
	cli.SetCommandSurface(restish.CommandSurface{PromotedAPI: "hast", SupportCommandNamespace: "cli"})
	cli.AddLoader(SpecLoader{})
	cli.AddAuthHandler(AuthType, &HeaderAuth{Session: session})
	return cli
}

func configureStatePathsFor(selection contextflow.Selection) {
	if selection.CacheDir != "" && (selection.Name != contextflow.DefaultName || os.Getenv("RSH_CACHE_DIR") == "") {
		_ = os.Setenv("RSH_CACHE_DIR", selection.CacheDir)
	}
}

func RunCLIWithContext(cli *restish.CLI, args []string, selection contextflow.Selection) error {
	configureStatePathsFor(selection)
	restore, err := restishengine.Isolate(cli, selection.ConfigDir)
	if err != nil {
		return err
	}
	defer restore()
	args, stripped := restishengine.FilterConfigFlags(args)
	if stripped {
		w := io.Writer(os.Stderr)
		if cli.Stderr != nil {
			w = cli.Stderr
		}
		fmt.Fprintln(w, "warning: --rsh-config is ignored; use hast auth or HAST_* variables")
	}
	return cli.Run(restishengine.ForceNoResponseCache(disableUnsafeRetries(args)))
}

func destructivePath(path string) bool {
	for _, part := range strings.Split(strings.ToLower(path), "/") {
		switch part {
		case "delete", "purge", "clear", "revoke":
			return true
		}
	}
	return false
}

// Task requests avoid accidental replay, even when Restish flags or environment
// opt into retrying unsafe requests. Read-only retries remain available.
func disableUnsafeRetries(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	result := []string{args[0], "--rsh-retry-unsafe=false"}
	for i, arg := range args[1:] {
		if arg == "--" {
			result = append(result, args[i+1:]...)
			break
		}
		if arg == "--rsh-retry-unsafe" || strings.HasPrefix(arg, "--rsh-retry-unsafe=") {
			continue
		}
		result = append(result, arg)
	}
	return result
}
