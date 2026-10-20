//go:build linux

package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type auditFile struct {
	file *os.File
	dir  int
	name string
	dev  uint64
	ino  uint64
}

func createAuditFile(path string) (*auditFile, error) {
	if path == "" || !utf8.ValidString(path) {
		return nil, errors.New("audit path must be a nonempty UTF-8 path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve audit path: %w", err)
	}
	clean := filepath.Clean(abs)
	if clean == string(filepath.Separator) {
		return nil, errors.New("audit path must name a new file")
	}
	parts := strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator))
	name := parts[len(parts)-1]
	if name == "" || name == "." || name == ".." {
		return nil, errors.New("audit path must name a new file")
	}
	euid := uint32(os.Geteuid())
	dirFD, err := unix.Open(string(filepath.Separator), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open audit path root: %w", err)
	}
	keepDir := false
	defer func() {
		if !keepDir {
			_ = unix.Close(dirFD)
		}
	}()
	for _, component := range parts[:len(parts)-1] {
		if component == "" || component == "." || component == ".." {
			return nil, errors.New("audit path contains an unsafe parent component")
		}
		next, err := unix.Openat(dirFD, component, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, fmt.Errorf("open audit parent component: %w", err)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(next, &stat); err != nil {
			_ = unix.Close(next)
			return nil, fmt.Errorf("inspect audit parent component: %w", err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || !trustedAuditDirectory(stat, euid) {
			_ = unix.Close(next)
			return nil, errors.New("audit parent must be a trusted, non-symlink directory")
		}
		_ = unix.Close(dirFD)
		dirFD = next
	}
	var parent unix.Stat_t
	if err := unix.Fstat(dirFD, &parent); err != nil || parent.Mode&unix.S_IFMT != unix.S_IFDIR || !trustedAuditDirectory(parent, euid) {
		if err != nil {
			return nil, fmt.Errorf("inspect audit parent: %w", err)
		}
		return nil, errors.New("audit parent must be a trusted directory")
	}
	fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create new audit file: %w", err)
	}
	file := os.NewFile(uintptr(fd), clean)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect new audit file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != euid {
		_ = file.Close()
		return nil, errors.New("audit output must be a regular file owned by the effective user")
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("set audit file mode: %w", err)
	}
	keepDir = true
	return &auditFile{file: file, dir: dirFD, name: name, dev: uint64(stat.Dev), ino: stat.Ino}, nil
}

func trustedAuditDirectory(stat unix.Stat_t, euid uint32) bool {
	if stat.Uid != 0 && stat.Uid != euid {
		return false
	}
	if stat.Mode&0o022 == 0 {
		return true
	}
	return stat.Uid == 0 && stat.Mode&unix.S_ISVTX != 0
}

func (a *auditFile) close() error {
	if a == nil {
		return nil
	}
	var errs []error
	if a.file != nil {
		if err := a.file.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync audit file: %w", err))
		}
		if err := a.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close audit file: %w", err))
		}
		a.file = nil
	}
	if a.dir >= 0 {
		if err := unix.Close(a.dir); err != nil {
			errs = append(errs, fmt.Errorf("close audit parent: %w", err))
		}
		a.dir = -1
	}
	return errors.Join(errs...)
}

func (a *auditFile) discard() error {
	if a == nil {
		return nil
	}
	var errs []error
	if a.file != nil {
		if err := a.file.Close(); err != nil {
			errs = append(errs, err)
		}
		a.file = nil
	}
	if a.dir >= 0 {
		var current unix.Stat_t
		if err := unix.Fstatat(a.dir, a.name, &current, unix.AT_SYMLINK_NOFOLLOW); err == nil && uint64(current.Dev) == a.dev && current.Ino == a.ino {
			if err := unix.Unlinkat(a.dir, a.name, 0); err != nil {
				errs = append(errs, err)
			}
		}
		if err := unix.Close(a.dir); err != nil {
			errs = append(errs, err)
		}
		a.dir = -1
	}
	return errors.Join(errs...)
}
