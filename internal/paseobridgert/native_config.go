package paseobridgert

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

type nativeHost struct {
	ServerID     string `json:"serverId"`
	PublicKey    string `json:"daemonPublicKeyB64"`
	PasswordFile string `json:"passwordFile,omitempty"`
}
type nativeConfig struct {
	Version  int                   `json:"version"`
	StateDir string                `json:"stateDir"`
	Hosts    map[string]nativeHost `json:"hosts"`
}

func loadNativeConfig(data []byte) (*nativeConfig, error) {
	var c nativeConfig
	if strictJSON(data, &c) != nil || c.Version != 2 || !filepath.IsAbs(c.StateDir) || len(c.Hosts) == 0 {
		return nil, fmt.Errorf("invalid native Relay configuration")
	}
	for host, pin := range c.Hosts {
		if !hostName.MatchString(host) || host == "all" || strings.TrimSpace(pin.ServerID) != pin.ServerID || pin.ServerID == "" || len(pin.ServerID) > 256 {
			return nil, fmt.Errorf("invalid native Relay host pin")
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(pin.PublicKey)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("invalid native Relay public key")
		}
		// Reject small-order Curve25519 pins before NaCl's unchecked precompute.
		secret := make([]byte, 32)
		secret[0] = 1
		private, _ := ecdh.X25519().NewPrivateKey(secret)
		public, _ := ecdh.X25519().NewPublicKey(raw)
		if _, err = private.ECDH(public); err != nil {
			return nil, fmt.Errorf("invalid native Relay public key")
		}
		if pin.PasswordFile != "" && !filepath.IsAbs(pin.PasswordFile) {
			return nil, fmt.Errorf("native Relay password path must be absolute")
		}
	}
	return &c, nil
}
