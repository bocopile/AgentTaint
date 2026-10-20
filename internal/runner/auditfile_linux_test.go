//go:build linux

package runner

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCreateAuditFileExclusive0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	audit, err := createAuditFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.file.Write([]byte("event\n")); err != nil {
		t.Fatal(err)
	}
	if err := audit.close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode = %04o, want 0600", info.Mode().Perm())
	}
	if _, err := createAuditFile(path); err == nil {
		t.Fatal("existing audit path was overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "event\n" {
		t.Fatalf("existing audit file changed: %q %v", data, err)
	}
}

func TestCreateAuditFileRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(root, "parent-link")
	if err := os.Symlink(real, parentLink); err != nil {
		t.Fatal(err)
	}
	finalLink := filepath.Join(root, "final-link")
	if err := os.Symlink(filepath.Join(real, "target"), finalLink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(real, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(parentLink, "audit.jsonl"),
		finalLink,
		fifo,
		filepath.Join(unsafe, "audit.jsonl"),
	} {
		if audit, err := createAuditFile(path); err == nil {
			_ = audit.discard()
			t.Errorf("unsafe path was accepted: %s", path)
		}
	}
}

func TestAuditDiscardDoesNotRemoveReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	audit, err := createAuditFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := audit.discard(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("discard removed replacement file: %q %v", data, err)
	}
}
