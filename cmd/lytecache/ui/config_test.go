package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateAuthConfigCreatesWith0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lytecache", "ui.yaml")

	cfg, created, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("expected created=true for a brand-new config file")
	}
	if cfg.Username != DefaultUsername {
		t.Errorf("Username = %q, want %q", cfg.Username, DefaultUsername)
	}
	if !cfg.IsDefaultPassword() {
		t.Error("a freshly bootstrapped config should have the default password")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %04o, want 0600", perm)
	}
}

func TestAuthConfigFileNeverContainsPlaintextPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lytecache", "ui.yaml")
	if _, _, err := LoadOrCreateAuthConfig(path); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// "username: admin" is expected and fine -- the username legitimately
	// is "admin". What must never appear is a *password* field holding
	// the plaintext value, and the schema shouldn't even have a "password:"
	// key (only "password_hash:") for one to leak into in the first place.
	if strings.Contains(string(raw), "password_hash: "+DefaultPassword+"\n") {
		t.Errorf("password_hash appears to hold a plaintext value:\n%s", raw)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "password:") {
			t.Errorf("config file has a bare \"password:\" field -- only password_hash should ever exist:\n%s", raw)
		}
	}
	if !strings.Contains(string(raw), "argon2id") {
		t.Errorf("expected an argon2id-encoded hash in the config file, got:\n%s", raw)
	}
}

func TestLoadOrCreateAuthConfigRefusesLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ui.yaml")
	if err := os.WriteFile(path, []byte("username: admin\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadOrCreateAuthConfig(path)
	if err == nil {
		t.Fatal("expected an error for a group/world-readable config file")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Errorf("error should tell the user how to fix it, got: %v", err)
	}
}

func TestLoadOrCreateAuthConfigLoadsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lytecache", "ui.yaml")
	first, created, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first call should report created=true")
	}

	second, created, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second call against an existing file should report created=false")
	}
	if second.SessionSecret != first.SessionSecret {
		t.Error("loading an existing config must not regenerate the session secret")
	}
}

func TestSaveAuthConfigPasswordChangeRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lytecache", "ui.yaml")
	cfg, _, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	newHash, err := HashPassword("a-brand-new-password")
	if err != nil {
		t.Fatal(err)
	}
	cfg.PasswordHash = newHash
	if err := SaveAuthConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	reloaded, _, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("a-brand-new-password", reloaded.PasswordHash) {
		t.Error("password change did not round-trip through save/reload")
	}
	if reloaded.IsDefaultPassword() {
		t.Error("reloaded config still reports the default password after a change")
	}
}

func TestSaveAuthConfigAlwaysWrites0600EvenIfLoosened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lytecache", "ui.yaml")
	cfg, _, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveAuthConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode after save = %04o, want 0600 -- save must not preserve a loosened mode", perm)
	}
}

func TestEnvPasswordOverrideSkipsDefault(t *testing.T) {
	t.Setenv(envPasswordOverride, "a-provisioned-password")
	path := filepath.Join(t.TempDir(), "lytecache", "ui.yaml")

	cfg, _, err := LoadOrCreateAuthConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsDefaultPassword() {
		t.Error("LYTECACHE_UI_PASSWORD should skip the default admin/admin password entirely")
	}
	if !VerifyPassword("a-provisioned-password", cfg.PasswordHash) {
		t.Error("the provisioned password was not what got hashed")
	}
}

func TestHashPasswordVerifyPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Error("VerifyPassword rejected the password it was hashed from")
	}
	if VerifyPassword("wrong password", hash) {
		t.Error("VerifyPassword accepted an incorrect password")
	}
}

func TestVerifyPasswordRejectsGarbage(t *testing.T) {
	if VerifyPassword("anything", "not-an-argon2-hash") {
		t.Error("VerifyPassword must reject a malformed hash, not panic or accept it")
	}
}

func TestCheckStartupGuardrails(t *testing.T) {
	defaultCfg := &AuthConfig{PasswordHash: mustHash(t, DefaultPassword)}
	changedCfg := &AuthConfig{PasswordHash: mustHash(t, "a-real-password")}

	cases := []struct {
		name          string
		host          string
		cfg           *AuthConfig
		tlsConfigured bool
		insecure      bool
		wantErr       bool
	}{
		{"loopback with default password is always fine", "127.0.0.1", defaultCfg, false, false, false},
		{"loopback ipv6 with default password is fine", "::1", defaultCfg, false, false, false},
		{"non-loopback with default password refused, not waivable by TLS", "0.0.0.0", defaultCfg, true, false, true},
		{"non-loopback with default password refused, not waivable by insecure", "0.0.0.0", defaultCfg, false, true, true},
		{"non-loopback with changed password but no TLS/insecure refused", "0.0.0.0", changedCfg, false, false, true},
		{"non-loopback with changed password and TLS is fine", "0.0.0.0", changedCfg, true, false, false},
		{"non-loopback with changed password and --insecure is fine", "0.0.0.0", changedCfg, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckStartupGuardrails(tc.host, tc.cfg, tc.tlsConfigured, tc.insecure)
			if (err != nil) != tc.wantErr {
				t.Errorf("CheckStartupGuardrails(%q, ..., tls=%v, insecure=%v) error = %v, wantErr %v",
					tc.host, tc.tlsConfigured, tc.insecure, err, tc.wantErr)
			}
		})
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
