package authflow_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xfe10/aicli/internal/authflow"
	"github.com/0xfe10/aicli/internal/fnsrt"
	"github.com/0xfe10/aicli/internal/pingcodert"
)

func TestStatusReportContractKeys(t *testing.T) {
	report := authflow.StatusReport{
		Configured:       true,
		Mode:             "token",
		BaseURL:          "https://example.test",
		BaseURLSource:    authflow.SourceConfig,
		CredentialSource: authflow.SourceConfig,
		ConfigPath:       "/tmp/config.toml",
	}
	var buf bytes.Buffer
	if err := authflow.WriteJSON(&buf, report); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"configured", "mode", "baseUrl", "baseUrlSource", "credentialSource", "configPath"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing status key %q in %s", key, buf.String())
		}
	}
}

func TestPingCodeAndFNSLoginRequireBaseURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		name string
		run  func(authflow.IO) error
	}{
		{"pingcode-client", func(io authflow.IO) error {
			return pingcodert.RunAuth([]string{"login", "--mode", "client"}, io)
		}},
		{"pingcode-token", func(io authflow.IO) error {
			return pingcodert.RunAuth([]string{"login", "--mode", "token"}, io)
		}},
		{"fns-token", func(io authflow.IO) error {
			return fnsrt.RunAuth([]string{"login", "--mode", "token"}, io)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := tc.run(authflow.IO{
				Stdin:  strings.NewReader("\n"),
				Stdout: &stdout,
				Stderr: &stdout,
				ReadSecret: func(string) (string, error) {
					t.Fatal("secret must not be prompted without Base URL")
					return "", nil
				},
			})
			if err == nil || !strings.Contains(err.Error(), "Base URL is required") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
