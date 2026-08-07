package ui

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultUsername/DefaultPassword are the first-run bootstrap credentials.
// Because they're publicly known (they're in this source file), every
// caller of these APIs must respect the guardrails documented on
// AuthConfig.IsDefaultPassword and CheckStartupGuardrails -- this is not
// optional hardening, it's the reason those guardrails exist at all.
const (
	DefaultUsername = "admin"
	DefaultPassword = "admin"
)

// envPasswordOverride, when set, is hashed on first read instead of using
// DefaultPassword -- for automated provisioning that skips the default
// entirely (see docs/ui.md).
const envPasswordOverride = "LYTECACHE_UI_PASSWORD"

// AuthConfig is the on-disk contents of ui.yaml: authentication and the
// operator-configured database list. It deliberately never contains a
// plaintext password, and deliberately does NOT contain --allow-delete --
// see Config.AllowDelete's doc comment for why that stays a CLI-flag-only
// setting instead of living here.
type AuthConfig struct {
	Username      string     `yaml:"username"`
	PasswordHash  string     `yaml:"password_hash"`
	SessionSecret string     `yaml:"session_secret"` // base64, HMAC key for session cookies
	MetricsToken  string     `yaml:"metrics_token,omitempty"`
	Databases     []DBSource `yaml:"databases,omitempty"`
}

// IsDefaultPassword reports whether the configured password still hashes
// to DefaultPassword. Checked on every startup and on every request to a
// non-loopback bind (not just at first creation) -- see
// CheckStartupGuardrails and the forced-change middleware in auth.go,
// since the password can change (or be reset) at any time via `lytecache
// ui passwd`/`reset-password` without necessarily restarting the server.
func (c *AuthConfig) IsDefaultPassword() bool {
	return VerifyPassword(DefaultPassword, c.PasswordHash)
}

// DefaultConfigDir returns "<OS config dir>/lytecache" (Linux
// $XDG_CONFIG_HOME or ~/.config, macOS ~/Library/Application Support,
// Windows %APPDATA%, via os.UserConfigDir).
func DefaultConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving platform config dir: %w", err)
	}
	return filepath.Join(dir, "lytecache"), nil
}

// DefaultConfigPath returns DefaultConfigDir()/ui.yaml.
func DefaultConfigPath() (string, error) {
	dir, err := DefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ui.yaml"), nil
}

// DefaultAuditLogPath returns DefaultConfigDir()/audit.log -- "next to the
// config", per the spec.
func DefaultAuditLogPath() (string, error) {
	dir, err := DefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "audit.log"), nil
}

// configFileMode is the only permission mode a config file (or the config
// directory holding it) is ever created or accepted with: owner
// read/write, nothing for group or other. Contains a password hash and a
// session-signing secret -- see CheckConfigPermissions.
const configFileMode = 0o600
const configDirMode = 0o700

// LoadOrCreateAuthConfig loads path, bootstrapping a fresh admin account
// (see DefaultUsername/DefaultPassword, or LYTECACHE_UI_PASSWORD to skip
// the default) if the file doesn't exist yet. created reports whether
// this call just created it, so a caller (cmd_ui.go) can decide whether to
// print the first-run credentials -- printing them on every subsequent
// startup, not just the first, would defeat the point of a warning.
func LoadOrCreateAuthConfig(path string) (cfg *AuthConfig, created bool, err error) {
	if err := CheckConfigPermissions(path); err != nil {
		return nil, false, err
	}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		cfg, err := bootstrapAuthConfig()
		if err != nil {
			return nil, false, err
		}
		if err := SaveAuthConfig(path, cfg); err != nil {
			return nil, false, err
		}
		return cfg, true, nil
	case err != nil:
		return nil, false, fmt.Errorf("reading %s: %w", path, err)
	}

	var loaded AuthConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return nil, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &loaded, false, nil
}

// SaveAuthConfig writes cfg to path as YAML, creating the parent directory
// if needed, always with configFileMode -- never wider, regardless of the
// process umask.
func SaveAuthConfig(path string, cfg *AuthConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), configDirMode); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	// os.WriteFile applies configFileMode only when *creating* the file;
	// an existing file (e.g. one an operator loosened by hand) keeps its
	// prior mode otherwise. Force it back explicitly on every save.
	return os.Chmod(path, configFileMode)
}

// CheckConfigPermissions refuses a config file that's group- or
// world-readable -- it holds a password hash and a session-signing
// secret. A not-yet-existing path is fine (it will be created with the
// right mode).
func CheckConfigPermissions(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if checkNotGroupOrWorldAccessible(info) {
		return fmt.Errorf("%s is group- or world-accessible (mode %04o) -- refusing to start; fix it with: chmod %04o %s",
			path, info.Mode().Perm(), configFileMode, path)
	}
	return nil
}

func bootstrapAuthConfig() (*AuthConfig, error) {
	secret, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("generating session secret: %w", err)
	}

	password := DefaultPassword
	if v := os.Getenv(envPasswordOverride); v != "" {
		password = v
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	return &AuthConfig{
		Username:      DefaultUsername,
		PasswordHash:  hash,
		SessionSecret: secret,
	}, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
