//go:build linux

package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"golang.org/x/sys/unix"
)

type auditFailureWriter struct {
	bytes.Buffer
	calls    int
	err      error
	output   io.Writer
	waitPath string
}

func (w *auditFailureWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.waitPath != "" {
		deadline := time.Now().Add(3 * time.Second)
		for {
			if _, err := os.Stat(w.waitPath); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return 0, syscall.ETIMEDOUT
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	n := 4
	if n > len(data) {
		n = len(data)
	}
	if w.output != nil {
		written, err := w.output.Write(data[:n])
		if err != nil {
			return written, err
		}
		n = written
	}
	_, _ = w.Buffer.Write(data[:n])
	return n, w.err
}

type auditShortWriter struct{ calls int }

func (w *auditShortWriter) Write(data []byte) (int, error) {
	w.calls++
	return len(data) - 1, nil
}

func auditWriterTestEvent() core.Event {
	return core.FileOpenEvent{
		Metadata: core.Metadata{
			SchemaVersion:    core.SchemaVersion,
			EventID:          "audit-writer-event",
			RunID:            core.RunID("audit-writer-run"),
			Timestamp:        time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
			SessionID:        core.SessionID(7),
			ObservationStage: core.StageCompleted,
			OperationResult:  core.ResultSuccess,
			Process: core.ProcessContext{
				ProcessKey: core.ProcessKey{PID: core.ProcessID(42), BirthID: "audit-writer-birth", ScopeID: "audit-writer-scope"},
				Comm:       "audit-writer-test",
			},
		},
		Resource: core.FileResource{Path: "/workspace/.env"},
		Access:   core.AccessRead,
	}
}

func TestAuditWriterDiskFailureIsSticky(t *testing.T) {
	output := &auditFailureWriter{err: syscall.ENOSPC}
	writer := &auditWriter{}
	event := auditWriterTestEvent()

	firstErr := writer.writeTo(event, output)
	if !errors.Is(firstErr, syscall.ENOSPC) {
		t.Fatalf("write error = %v, want ENOSPC", firstErr)
	}
	if writer.written != int64(output.Len()) || output.Len() == 0 {
		t.Fatalf("partial bytes were not accounted for: written=%d output=%d", writer.written, output.Len())
	}
	if got := writer.writeTo(event, output); got != firstErr {
		t.Fatalf("sticky error = %v, want original %v", got, firstErr)
	}
	if output.calls != 1 {
		t.Fatalf("writer called %d times after sticky failure, want 1", output.calls)
	}
}

func TestAuditWriterShortWriteIsSticky(t *testing.T) {
	output := &auditShortWriter{}
	writer := &auditWriter{}
	event := auditWriterTestEvent()

	firstErr := writer.writeTo(event, output)
	if !errors.Is(firstErr, io.ErrShortWrite) {
		t.Fatalf("write error = %v, want ErrShortWrite", firstErr)
	}
	if got := writer.writeTo(event, output); got != firstErr {
		t.Fatalf("sticky error = %v, want original %v", got, firstErr)
	}
	if output.calls != 1 {
		t.Fatalf("writer called %d times after sticky failure, want 1", output.calls)
	}
}

func TestAuditWriterCapBoundaryAndFailureAreSticky(t *testing.T) {
	event := auditWriterTestEvent()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	eventBytes := int64(len(encoded) + 1)
	output := &bytes.Buffer{}
	writer := &auditWriter{written: maxAuditBytes - eventBytes}

	if err := writer.writeTo(event, output); err != nil {
		t.Fatalf("write at exact audit cap failed: %v", err)
	}
	if writer.written != maxAuditBytes || output.Len() != int(eventBytes) {
		t.Fatalf("exact cap state: written=%d output=%d", writer.written, output.Len())
	}
	firstErr := writer.writeTo(event, output)
	if firstErr == nil || !errors.Is(firstErr, writer.failed) {
		t.Fatalf("write beyond audit cap error = %v, sticky=%v", firstErr, writer.failed)
	}
	if got := writer.writeTo(event, output); got != firstErr {
		t.Fatalf("sticky error = %v, want original %v", got, firstErr)
	}
	if output.Len() != int(eventBytes) {
		t.Fatalf("cap failure wrote extra bytes: got=%d want=%d", output.Len(), eventBytes)
	}
}

func trustedAuditTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreateAuditFileExclusive0600(t *testing.T) {
	dir := trustedAuditTempDir(t)
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
	root := trustedAuditTempDir(t)
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
	dir := trustedAuditTempDir(t)
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
