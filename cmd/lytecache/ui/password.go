package ui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. These follow the widely-used "interactive" profile
// (OWASP's password storage cheat sheet lists m=19MiB/t=2 or m=64MiB/t=1
// as equivalent-strength baselines for a login path that must stay
// responsive) -- 64 MiB of memory, 1 pass, 4-way parallelism, a 16-byte
// salt, and a 32-byte derived key. Pure Go (golang.org/x/crypto/argon2),
// so this has no effect on the CGO_ENABLED=0 build.
const (
	argon2Time    = 1
	argon2MemoryK = 64 * 1024 // KiB, i.e. 64 MiB
	argon2Threads = 4
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

// HashPassword returns a self-describing argon2id hash string (PHC-like
// format: $argon2id$v=<version>$m=<mem>,t=<time>,p=<threads>$<salt>$<hash>,
// each component base64-encoded without padding) suitable for storing in
// AuthConfig.PasswordHash. The plaintext password is never retained.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2MemoryK, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2MemoryK, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether password matches encoded (as produced by
// HashPassword), re-deriving the hash with the parameters/salt embedded in
// encoded itself -- so a future change to argon2MemoryK etc. doesn't break
// verifying passwords hashed under the old parameters. An invalid/corrupt
// encoded string is treated as "does not match", not an error: this is a
// login/verification path, not a parser API.
func VerifyPassword(password, encoded string) bool {
	salt, hash, memory, timeCost, threads, err := decodeArgon2Hash(encoded)
	if err != nil {
		return false
	}
	candidate := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(hash)))
	return subtle.ConstantTimeCompare(candidate, hash) == 1
}

func decodeArgon2Hash(encoded string) (salt, hash []byte, memory, timeCost uint32, threads uint8, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, fmt.Errorf("ui: not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("ui: parsing argon2 version: %w", err)
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("ui: parsing argon2 params: %w", err)
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("ui: decoding argon2 salt: %w", err)
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("ui: decoding argon2 hash: %w", err)
	}
	return salt, hash, memory, timeCost, threads, nil
}
