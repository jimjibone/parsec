// Package config loads the optional parsec server config file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Auth modes.
const (
	ModeNone  = "none"  // single user, no sign-in (localhost use)
	ModeOIDC  = "oidc"  // OpenID Connect, e.g. GitLab
	ModeLocal = "local" // usernames and passwords kept in the state dir
)

type Config struct {
	Addr string `yaml:"addr"`
	Data string `yaml:"data"`
	// PublicURL is the address users open, e.g. https://parsec.example.com.
	// Required for oidc (it forms the redirect URL) and used to check the
	// Origin of write requests.
	PublicURL string `yaml:"publicURL"`
	// StateDir holds users.yaml and the session key. It must be outside Data.
	StateDir string `yaml:"stateDir"`
	Auth     Auth   `yaml:"auth"`
	Git      Git    `yaml:"git"`
}

type Auth struct {
	Mode string `yaml:"mode"`
	// Admins are usernames that are always admins, so access can never be
	// locked out from the settings panel.
	Admins []string `yaml:"admins"`
	// DefaultRole is the initial role for new sign-ins (viewer, editor or
	// none). Admins can change it in the settings panel afterwards.
	DefaultRole string `yaml:"defaultRole"`
	SessionDays int    `yaml:"sessionDays"`
	OIDC        OIDC   `yaml:"oidc"`
}

type OIDC struct {
	Issuer   string `yaml:"issuer"`
	ClientID string `yaml:"clientID"`
	// ClientSecret may also come from ClientSecretFile or the
	// PARSEC_OIDC_CLIENT_SECRET environment variable.
	ClientSecret     string   `yaml:"clientSecret"`
	ClientSecretFile string   `yaml:"clientSecretFile"`
	Scopes           []string `yaml:"scopes"`
	// Label names the provider on the sign-in button.
	Label string `yaml:"label"`
}

type Git struct {
	// AutoCommit commits each user's changes after they stop editing for
	// Idle. Defaults to on when sign-in is enabled.
	AutoCommit *bool         `yaml:"autoCommit"`
	Idle       time.Duration `yaml:"idle"`
	// Push syncs (pull --rebase, then push) after each automatic commit
	// when a remote is set.
	Push *bool `yaml:"push"`
}

// Load reads path (if non-empty) and fills in defaults.
func Load(path string) (*Config, error) {
	c := &Config{}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		// io.EOF: an empty file is fine.
		if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return c, nil
}

// Finish applies defaults and validates. Call after flag overrides.
func (c *Config) Finish() error {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:7343"
	}
	if c.Data == "" {
		c.Data = "parsec-data"
	}
	if c.Auth.Mode == "" {
		c.Auth.Mode = ModeNone
	}
	if c.Auth.DefaultRole == "" {
		c.Auth.DefaultRole = "viewer"
	}
	if c.Auth.SessionDays == 0 {
		c.Auth.SessionDays = 30
	}
	if c.Git.Idle == 0 {
		c.Git.Idle = 5 * time.Minute
	}
	shared := c.Auth.Mode != ModeNone
	if c.Git.AutoCommit == nil {
		c.Git.AutoCommit = &shared
	}
	if c.Git.Push == nil {
		c.Git.Push = &shared
	}
	if len(c.Auth.OIDC.Scopes) == 0 {
		c.Auth.OIDC.Scopes = []string{"openid", "profile", "email"}
	}
	if c.Auth.OIDC.Label == "" {
		c.Auth.OIDC.Label = "single sign-on"
	}

	var err error
	if c.Data, err = filepath.Abs(c.Data); err != nil {
		return err
	}

	switch c.Auth.DefaultRole {
	case "viewer", "editor", "none":
	default:
		return fmt.Errorf("auth.defaultRole must be viewer, editor or none, got %q", c.Auth.DefaultRole)
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("publicURL must be an absolute URL, got %q", c.PublicURL)
		}
		c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	}

	switch c.Auth.Mode {
	case ModeNone:
		return nil
	case ModeOIDC:
		o := &c.Auth.OIDC
		if s := os.Getenv("PARSEC_OIDC_CLIENT_SECRET"); s != "" {
			o.ClientSecret = s
		}
		if o.ClientSecret == "" && o.ClientSecretFile != "" {
			b, err := os.ReadFile(o.ClientSecretFile)
			if err != nil {
				return fmt.Errorf("auth.oidc.clientSecretFile: %w", err)
			}
			o.ClientSecret = strings.TrimSpace(string(b))
		}
		if o.Issuer == "" || o.ClientID == "" || o.ClientSecret == "" {
			return errors.New("auth.oidc needs issuer, clientID and a client secret")
		}
		if c.PublicURL == "" {
			return errors.New("publicURL is required for oidc sign-in")
		}
	case ModeLocal:
	default:
		return fmt.Errorf("auth.mode must be none, oidc or local, got %q", c.Auth.Mode)
	}

	if c.StateDir == "" {
		return errors.New("stateDir is required when sign-in is enabled")
	}
	if c.StateDir, err = filepath.Abs(c.StateDir); err != nil {
		return err
	}
	if rel, err := filepath.Rel(c.Data, c.StateDir); err == nil && !strings.HasPrefix(rel, "..") {
		return errors.New("stateDir must be outside the data directory, or it would be committed")
	}
	return nil
}

// Shared reports whether several users sign in (any mode but none).
func (c *Config) Shared() bool { return c.Auth.Mode != ModeNone }
