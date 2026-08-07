package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogDirIsOSAppropriate(t *testing.T) {
	isolateHome(t)
	dir, err := LogDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Fatal("LogDir() returned an empty path")
	}
	// Every branch's result must at least be an absolute path under the
	// isolated HOME/LOCALAPPDATA this test set up -- not the real machine's.
	if !filepath.IsAbs(dir) {
		t.Errorf("LogDir() = %q, want an absolute path", dir)
	}
}

func TestRotatingWriterRotatesPastMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewRotatingWriter(path, 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	// Each write is 10 bytes; the third write pushes cumulative size past
	// maxBytes=20, triggering a rotation before it's written.
	line := []byte("0123456789")
	for i := 0; i < 3; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected the active log file to exist: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected a .1 backup after rotation: %v", err)
	}
}

func TestRotatingWriterRespectsMaxBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewRotatingWriter(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	// Force several rotations -- each write is exactly at the boundary,
	// so every single write after the first rotates.
	line := []byte("0123456789")
	for i := 0; i < 6; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	if _, err := os.Stat(path + ".3"); err == nil {
		t.Error("expected no .3 backup -- maxBackups=2 should cap it at .1/.2")
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Errorf("expected a .2 backup to exist: %v", err)
	}
}

func TestRotatingWriterAppendsAcrossReopens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w1, err := NewRotatingWriter(path, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w1.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := w1.Close(); err != nil {
		t.Fatal(err)
	}

	w2, err := NewRotatingWriter(path, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w2.Close() }()
	if _, err := w2.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\n" {
		t.Errorf("log content = %q, want both writes preserved across reopen", data)
	}
}
