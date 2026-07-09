package main

import (
	"database/sql"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	lytecache "github.com/lytecache/lytecache-go"
)

// buildFixtureDB creates a database file with the schema initialized (via
// the real library, so it's byte-for-byte what every implementation
// produces -- see SPEC.md), then inserts one row per value_type code (0-6)
// directly via raw SQL. Codes 5/6 (python-pickle/java-serialized) can only
// be produced this way: the library itself never writes them, so this is
// the one place the CLI's tests reach past the public API, on purpose, to
// exercise "a database written by another language or an old/foreign
// process".
func buildFixtureDB(t *testing.T) string {
	t.Helper()
	path := tempDBPath(t)

	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UnixMilli()
	const insert = `
INSERT INTO cache (key, namespace, value, value_type, created_at, expires_at, last_accessed, access_count, size_bytes)
VALUES (?, 'default', ?, ?, ?, NULL, ?, 0, ?)`

	rows := []struct {
		key   string
		value []byte
		code  int
	}{
		{"k-bytes", []byte{0x01, 0x02, 0x03}, typeBytes},
		{"k-string", []byte("hello"), typeString},
		{"k-int", []byte("42"), typeInt},
		{"k-float", []byte("3.14"), typeFloat},
		{"k-json", []byte(`{"a":1}`), typeJSON},
		{"k-python", []byte{0x80, 0x04, 0x95, 0x00, 0x01}, typePython},
		{"k-java", []byte{0xac, 0xed, 0x00, 0x05, 0x77}, typeJava},
	}
	for _, row := range rows {
		if _, err := db.Exec(insert, row.key, row.value, row.code, now, now, len(row.value)); err != nil {
			t.Fatalf("inserting fixture row %s: %v", row.key, err)
		}
	}

	return path
}

func TestFixtureGetRendersEveryTypeCode(t *testing.T) {
	db := buildFixtureDB(t)

	cases := []struct {
		key  string
		want string
	}{
		{"k-bytes", base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}) + "\n(bytes, 3 B)\n"},
		{"k-string", "hello\n"},
		{"k-int", "42\n"},
		{"k-float", "3.14\n"},
		{"k-json", "{\n  \"a\": 1\n}\n"},
		{"k-python", "(non-portable value: python-pickle, 5 bytes)\n"},
		{"k-java", "(non-portable value: java-serialized, 5 bytes)\n"},
	}
	for _, tc := range cases {
		r := runCLI(t, "", "--db", db, "get", tc.key)
		if r.code != exitSuccess {
			t.Errorf("get %s: code=%d stderr=%s", tc.key, r.code, r.stderr)
			continue
		}
		if r.stdout != tc.want {
			t.Errorf("get %s stdout = %q, want %q", tc.key, r.stdout, tc.want)
		}
	}
}

func TestFixtureGetRawBytes(t *testing.T) {
	db := buildFixtureDB(t)
	r := runCLI(t, "", "--db", db, "get", "k-bytes", "--raw")
	if r.code != exitSuccess {
		t.Fatalf("get --raw: code=%d stderr=%s", r.code, r.stderr)
	}
	if r.stdout != "\x01\x02\x03" {
		t.Errorf("stdout = %q, want raw bytes", r.stdout)
	}
}

func TestFixtureDumpRendersEveryTypeCode(t *testing.T) {
	db := buildFixtureDB(t)

	wantNames := map[string]string{
		"k-bytes":  "bytes",
		"k-string": "string",
		"k-int":    "int",
		"k-float":  "float",
		"k-json":   "json",
		"k-python": "python-pickle",
		"k-java":   "java-serialized",
	}
	for key, name := range wantNames {
		r := runCLI(t, "", "--db", db, "dump", key)
		if r.code != exitSuccess {
			t.Errorf("dump %s: code=%d stderr=%s", key, r.code, r.stderr)
			continue
		}
		if !strings.Contains(r.stdout, name) {
			t.Errorf("dump %s stdout = %q, want it to mention %q", key, r.stdout, name)
		}
	}

	r := runCLI(t, "", "--db", db, "dump", "k-python")
	if !strings.Contains(r.stdout, "non-portable value: python-pickle, 5 bytes") {
		t.Errorf("dump k-python stdout = %q, want the non-portable message", r.stdout)
	}
}

func TestFixtureKeysLongRendersEveryTypeCode(t *testing.T) {
	db := buildFixtureDB(t)

	r := runCLI(t, "", "--db", db, "keys", "--long")
	if r.code != exitSuccess {
		t.Fatalf("keys --long: code=%d stderr=%s", r.code, r.stderr)
	}
	for _, name := range []string{"bytes", "string", "int", "float", "json", "python-pickle", "java-serialized"} {
		if !strings.Contains(r.stdout, name) {
			t.Errorf("keys --long stdout missing type %q:\n%s", name, r.stdout)
		}
	}
}
