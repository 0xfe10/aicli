// Package itsaplanrt binds the itsaplan OpenAPI to the embedded Restish runtime.
package itsaplanrt

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
	DefaultBaseURL     = "https://its-api.kahub.in"
	PlaceholderHost    = "itsaplan.example.com"
	AuthType           = "itsaplan-key"
	securitySchemeName = "apiKey"
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
		spec = base + "/docs/json"
	}
	spec, err = authflow.NormalizeBaseURL(spec)
	if err != nil {
		return Session{}, Config{}, fmt.Errorf("ITSAPLAN_SPEC_URL: %w", err)
	}
	return session, Config{BaseURL: base, SpecURL: spec}, nil
}

func NewCLIWithContext(cfg Config, session Session, version, commit string, selection contextflow.Selection) *restish.CLI {
	cli := restish.New()
	cli.SetCommandName("itsaplan")
	cli.SetCommandDescription("It's a Plan API CLI", "Commands generated from the selected instance's OpenAPI.\nLocal commands: auth login|status|logout; context current|list|use.\nITSAPLAN_WRITE_MODE: readonly (default), write, destructive.\n--rsh-config is ignored; use config.toml or ITSAPLAN_* variables.")
	cli.SetVersion(version + " (" + commit + ")")
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{"itsaplan": {
		BaseURL: cfg.BaseURL, SpecURL: cfg.SpecURL, CommandLayout: "tags",
		Profiles: map[string]*restish.ProfileConfig{"default": {Credentials: map[string]*restishconfig.CredentialConfig{securitySchemeName: {Auth: &restish.AuthConfig{Type: AuthType}}}}},
	}}})
	cli.SetCommandSurface(restish.CommandSurface{PromotedAPI: "itsaplan", SupportCommandNamespace: "cli"})
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
		fmt.Fprintln(w, "warning: --rsh-config is ignored; use itsaplan auth or ITSAPLAN_* variables")
	}
	return cli.Run(restishengine.ForceNoResponseCache(args))
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
