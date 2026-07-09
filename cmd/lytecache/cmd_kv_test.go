package main

import (
	"strings"
	"testing"
)

func TestGetSetRoundTrip(t *testing.T) {
	db := tempDBPath(t)

	if r := runCLI(t, "", "--db", db, "set", "user:1", `{"name":"Ada"}`); r.code != exitSuccess {
		t.Fatalf("set: code=%d stderr=%s", r.code, r.stderr)
	}

	r := runCLI(t, "", "--db", db, "get", "user:1")
	if r.code != exitSuccess {
		t.Fatalf("get: code=%d stderr=%s", r.code, r.stderr)
	}
	want := "{\n  \"name\": \"Ada\"\n}\n"
	if r.stdout != want {
		t.Errorf("get stdout = %q, want %q", r.stdout, want)
	}
}

func TestGetMiss(t *testing.T) {
	db := tempDBPath(t)
	// A read-only command against a not-yet-created database is a database
	// error (exit 3), not a miss -- create the file first with a write.
	runCLI(t, "", "--db", db, "set", "other", "1")

	r := runCLI(t, "", "--db", db, "get", "missing")
	if r.code != exitFalseOrMiss {
		t.Fatalf("code = %d, want %d (stderr=%s)", r.code, exitFalseOrMiss, r.stderr)
	}
	if r.stdout != "(nil)\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "(nil)\n")
	}
}

func TestGetAgainstMissingDatabase(t *testing.T) {
	db := tempDBPath(t)
	r := runCLI(t, "", "--db", db, "get", "anything")
	if r.code != exitDatabase {
		t.Fatalf("code = %d, want %d (stdout=%s stderr=%s)", r.code, exitDatabase, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, db) {
		t.Errorf("stderr = %q, want it to mention the resolved path %q", r.stderr, db)
	}
}

func TestSetCreatesDatabase(t *testing.T) {
	db := tempDBPath(t)
	if r := runCLI(t, "", "--db", db, "set", "k", "v"); r.code != exitSuccess {
		t.Fatalf("set: code=%d stderr=%s", r.code, r.stderr)
	}
	if r := runCLI(t, "", "--db", db, "get", "k"); r.code != exitSuccess || r.stdout != "v\n" {
		t.Fatalf("get: code=%d stdout=%q stderr=%s", r.code, r.stdout, r.stderr)
	}
}

func TestSetTypeInference(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "int", "42")
	runCLI(t, "", "--db", db, "set", "float", "3.14")
	runCLI(t, "", "--db", db, "set", "str", "hello")
	runCLI(t, "", "--db", db, "set", "json", `[1,2,3]`)

	cases := map[string]string{
		"int":   "42\n",
		"float": "3.14\n",
		"str":   "hello\n",
		"json":  "[\n  1,\n  2,\n  3\n]\n",
	}
	for key, want := range cases {
		r := runCLI(t, "", "--db", db, "get", key)
		if r.stdout != want {
			t.Errorf("get %s stdout = %q, want %q", key, r.stdout, want)
		}
	}
}

func TestSetWithTTL(t *testing.T) {
	db := tempDBPath(t)
	if r := runCLI(t, "", "--db", db, "set", "k", "v", "--ttl", "300"); r.code != exitSuccess {
		t.Fatalf("set: code=%d stderr=%s", r.code, r.stderr)
	}
	r := runCLI(t, "", "--db", db, "ttl", "k")
	if r.code != exitSuccess {
		t.Fatalf("ttl: code=%d stderr=%s", r.code, r.stderr)
	}
	seconds := strings.TrimSpace(r.stdout)
	if seconds == "-1" || seconds == "" {
		t.Errorf("ttl stdout = %q, want a positive remaining-seconds value", r.stdout)
	}
}

func TestSetTypeBytesBase64(t *testing.T) {
	db := tempDBPath(t)
	// base64 of "hi"
	if r := runCLI(t, "", "--db", db, "set", "k", "aGk=", "--type", "bytes"); r.code != exitSuccess {
		t.Fatalf("set: code=%d stderr=%s", r.code, r.stderr)
	}
	r := runCLI(t, "", "--db", db, "get", "k", "--raw")
	if r.code != exitSuccess {
		t.Fatalf("get --raw: code=%d stderr=%s", r.code, r.stderr)
	}
	if r.stdout != "hi" {
		t.Errorf("get --raw stdout = %q, want %q", r.stdout, "hi")
	}
}

func TestSetTypeBytesFromStdin(t *testing.T) {
	db := tempDBPath(t)
	if r := runCLI(t, "hello from stdin", "--db", db, "set", "k", "-", "--type", "bytes"); r.code != exitSuccess {
		t.Fatalf("set: code=%d stderr=%s", r.code, r.stderr)
	}
	r := runCLI(t, "", "--db", db, "get", "k", "--raw")
	if r.stdout != "hello from stdin" {
		t.Errorf("stdout = %q, want %q", r.stdout, "hello from stdin")
	}
}

func TestSetInvalidType(t *testing.T) {
	db := tempDBPath(t)
	r := runCLI(t, "", "--db", db, "set", "k", "v", "--type", "bogus")
	if r.code != exitUsage {
		t.Fatalf("code = %d, want %d (stderr=%s)", r.code, exitUsage, r.stderr)
	}
}

func TestDel(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "a", "1")
	runCLI(t, "", "--db", db, "set", "b", "2")

	r := runCLI(t, "", "--db", db, "del", "a", "b", "missing")
	if r.code != exitSuccess {
		t.Fatalf("del: code=%d stderr=%s", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "2" {
		t.Errorf("del stdout = %q, want count 2", r.stdout)
	}

	if r := runCLI(t, "", "--db", db, "exists", "a"); r.code != exitFalseOrMiss {
		t.Errorf("exists after del: code=%d, want %d", r.code, exitFalseOrMiss)
	}
}

func TestExists(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "exists", "k")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "1" {
		t.Errorf("exists k: code=%d stdout=%q", r.code, r.stdout)
	}

	r = runCLI(t, "", "--db", db, "exists", "missing")
	if r.code != exitFalseOrMiss || strings.TrimSpace(r.stdout) != "0" {
		t.Errorf("exists missing: code=%d stdout=%q", r.code, r.stdout)
	}
}
