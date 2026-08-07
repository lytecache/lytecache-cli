package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// LogDir returns the OS-appropriate log directory for the lytecache-ui
// service: ~/Library/Logs/lytecache (macOS), $XDG_STATE_HOME/lytecache or
// ~/.local/state/lytecache (Linux and other Unix), %LOCALAPPDATA%\lytecache\logs
// (Windows).
func LogDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Logs", "lytecache"), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "lytecache", "logs"), nil
	default:
		if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
			return filepath.Join(xdg, "lytecache"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		return filepath.Join(home, ".local", "state", "lytecache"), nil
	}
}

// LogPath returns LogDir()/lytecache-ui.log.
func LogPath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lytecache-ui.log"), nil
}

// DefaultMaxLogBytes/DefaultMaxLogBackups bound the rotated log's total
// disk footprint (default cap: 10MiB x 6 files = 60MiB) -- a local admin
// tool's own log volume (one line per write action, plus startup/
// shutdown messages) is inherently small; this exists to bound an
// unexpectedly chatty run, not to manage a high-volume log.
const (
	DefaultMaxLogBytes   = 10 * 1024 * 1024
	DefaultMaxLogBackups = 5
)

// RotatingWriter is a minimal size-based log rotator: once the current
// file would exceed maxBytes, existing numbered backups shift up by one
// (dropping the oldest past maxBackups) and a fresh file starts. This is
// intentionally simple, not a full-featured rotation library -- see this
// package's doc comment on log volume.
type RotatingWriter struct {
	path       string
	maxBytes   int64
	maxBackups int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// NewRotatingWriter opens (creating if needed) path for appending, sized
// against maxBytes/maxBackups.
func NewRotatingWriter(path string, maxBytes int64, maxBackups int) (*RotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening log file %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &RotatingWriter{path: path, maxBytes: maxBytes, maxBackups: maxBackups, f: f, size: info.Size()}, nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) rotateLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	for i := w.maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		dst := fmt.Sprintf("%s.%d", w.path, i+1)
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}
	if w.maxBackups > 0 {
		_ = os.Rename(w.path, w.path+".1")
	}
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	w.size = 0
	return nil
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
