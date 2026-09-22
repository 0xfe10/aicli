package lanhurt

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/0xfe10/aicli/internal/contextflow"
	"github.com/0xfe10/aicli/internal/restishengine"
	restish "github.com/rest-sh/restish/v2"
	restishconfig "github.com/rest-sh/restish/v2/config"
)

//go:embed openapi.yaml
var openAPISpec []byte

func NewCLI(session Session, selection contextflow.Selection, version, commit string) (*restish.CLI, error) {
	specPath, err := materializeSpec(selection.ConfigDir)
	if err != nil {
		return nil, err
	}
	cli := restish.New()
	cli.SetCommandName("lanhu")
	cli.SetCommandDescription("Lanhu API and design workflow CLI", "Local commands: auth, context, design, and axure. Raw read-only API commands are generated from the embedded observed contract.")
	cli.SetVersion(version + " (" + commit + ")")
	cli.SetDefaultConfig(&restish.Config{APIs: map[string]*restish.APIConfig{"lanhu": {
		BaseURL: DefaultBaseURL, SpecFiles: []string{specPath}, CommandLayout: "tags",
		AllowedOperationOrigins: []string{"https://dds.lanhuapp.com"},
		Profiles:                map[string]*restish.ProfileConfig{"default": {Credentials: map[string]*restishconfig.CredentialConfig{"lanhuCookie": {Auth: &restish.AuthConfig{Type: AuthType}}}}},
	}}})
	cli.SetCommandSurface(restish.CommandSurface{PromotedAPI: "lanhu", SupportCommandNamespace: "cli"})
	cli.AddAuthHandler(AuthType, &HeaderAuth{Session: session})
	return cli, nil
}

func RunCLI(cli *restish.CLI, args []string, selection contextflow.Selection) error {
	if os.Getenv("RSH_CACHE_DIR") == "" && selection.CacheDir != "" {
		_ = os.Setenv("RSH_CACHE_DIR", selection.CacheDir)
	}
	restore, err := restishengine.Isolate(cli, selection.ConfigDir)
	if err != nil {
		return err
	}
	defer restore()
	filtered, stripped := restishengine.FilterConfigFlags(args)
	if stripped {
		writer := io.Writer(os.Stderr)
		if cli.Stderr != nil {
			writer = cli.Stderr
		}
		fmt.Fprintln(writer, "warning: --rsh-config is ignored; Lanhu configuration comes from config.toml / environment variables")
	}
	return cli.Run(restishengine.ForceNoResponseCache(filtered))
}

func materializeSpec(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("Lanhu state directory is unavailable")
	}
	engine := filepath.Join(dir, "engine")
	if err := os.MkdirAll(engine, 0o700); err != nil {
		return "", err
	}
	if info, err := os.Lstat(engine); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("Lanhu engine state must be a directory, not a symlink")
	}
	path := filepath.Join(engine, "openapi.yaml")
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("Lanhu OpenAPI cache must not be a symlink")
	}
	if err := os.WriteFile(path, openAPISpec, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
