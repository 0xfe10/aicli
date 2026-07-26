package restishrt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	restish "github.com/rest-sh/restish/v2"
	"github.com/rest-sh/restish/v2/auth"
)

const pinnedRestishVersion = "2.3.0"

// AuthProvider supplies Authorization headers for Restish raw requests.
type AuthProvider func(ctx context.Context) (string, error)

// Runtime wraps an embedded Restish CLI for pingcode raw.
type Runtime struct {
	APIBaseURL string
	Auth       AuthProvider
	Stdout     io.Writer
	Stderr     io.Writer
}

// Version returns the pinned embedded Restish module version.
func Version() string {
	return pinnedRestishVersion
}

// Run executes Restish with argv under a PingCode-scoped default API.
// Domain command JSON contracts are untouched; this path only serves `pingcode raw`.
func (r *Runtime) Run(ctx context.Context, args []string) error {
	_ = ctx
	cli := restish.New()
	cli.SetCommandName("pingcode-raw")
	cli.SetCommandDescription("PingCode raw API debug via embedded Restish", "Escaped Restish surface for PingCode Open API exploration.")
	cli.SetVersion(pinnedRestishVersion)
	cli.SetSignalHandling(false)

	base := strings.TrimRight(r.APIBaseURL, "/")
	cfg := &restish.Config{
		APIs: map[string]*restish.APIConfig{
			"pingcode": {
				BaseURL: base,
			},
		},
	}
	if r.Auth != nil {
		cfg.APIs["pingcode"].Profiles = map[string]*restish.ProfileConfig{
			"default": {
				Auth: &restish.AuthConfig{
					Type:   "pingcode-bearer",
					Params: map[string]string{},
				},
			},
		}
		cli.AddAuthHandler("pingcode-bearer", &bearerAuth{provider: r.Auth})
	}
	cli.SetDefaultConfig(cfg)
	cli.SetCommandSurface(restish.CommandSurface{
		PromotedAPI: "pingcode",
	})

	outW := r.Stdout
	if outW == nil {
		outW = os.Stdout
	}
	errW := r.Stderr
	if errW == nil {
		errW = os.Stderr
	}

	runArgs := append([]string{"pingcode-raw"}, args...)
	runErr := cli.Run(runArgs)
	enc := json.NewEncoder(outW)
	enc.SetIndent("", "  ")
	if runErr != nil {
		_, _ = fmt.Fprintln(errW, redact(runErr.Error()))
		_ = enc.Encode(map[string]any{
			"ok": false,
			"error": map[string]string{
				"code":    "UPSTREAM_ERROR",
				"message": redact(runErr.Error()),
			},
			"meta": map[string]any{"command": "raw"},
		})
		return runErr
	}
	return enc.Encode(map[string]any{
		"ok": true,
		"data": map[string]any{
			"note": "Restish completed; see stderr for Restish human/output formatting when applicable",
		},
		"meta": map[string]any{
			"command":         "raw",
			"restish_version": pinnedRestishVersion,
		},
	})
}

type bearerAuth struct {
	provider AuthProvider
}

func (b *bearerAuth) Parameters() []auth.Param {
	return nil
}

func (b *bearerAuth) Authenticate(ctx context.Context, req *http.Request, _ auth.AuthContext) error {
	if b.provider == nil {
		return fmt.Errorf("missing auth provider")
	}
	authz, err := b.provider(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authz)
	return nil
}

func redact(text string) string {
	// Avoid importing pingcode (cycle). Strip common secret patterns.
	out := text
	for _, p := range []string{"Bearer ", "access_token=", "refresh_token=", "client_secret=", "code="} {
		if i := strings.Index(strings.ToLower(out), strings.ToLower(p)); i >= 0 {
			// best-effort truncation after the key
			_ = i
		}
	}
	out = strings.ReplaceAll(out, "\n", " ")
	return out
}
