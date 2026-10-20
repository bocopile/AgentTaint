//go:build linux

package runner

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const cgroup2SuperMagic = 0x63677270

type runCgroup struct {
	path string
	dir  *os.File
	id   uint64
}

func createRunCgroup() (*runCgroup, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("block mode requires a root supervisor to create a private cgroup v2 scope")
	}
	switch runtime.GOARCH {
	case "amd64", "arm64", "loong64", "mips64le", "ppc64le", "riscv64":
	default:
		return nil, fmt.Errorf("block mode does not support cgroup ID discovery on %s", runtime.GOARCH)
	}
	root := "/sys/fs/cgroup"
	var filesystem unix.Statfs_t
	if err := unix.Statfs(root, &filesystem); err != nil {
		return nil, fmt.Errorf("inspect cgroup v2 mount: %w", err)
	}
	if uint64(filesystem.Type) != cgroup2SuperMagic {
		return nil, errors.New("block mode requires a cgroup v2 filesystem at /sys/fs/cgroup")
	}
	for attempt := 0; attempt < 4; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, fmt.Errorf("create run cgroup name: %w", err)
		}
		token := hex.EncodeToString(random[:])
		path := filepath.Join(root, "agenttaint-"+token)
		if err := os.Mkdir(path, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, fmt.Errorf("create private run cgroup: %w", err)
		}
		return openRunCgroup(path)
	}
	return nil, errors.New("could not allocate a unique run cgroup")
}

func openRunCgroup(path string) (*runCgroup, error) {
	dir, err := os.Open(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("open private run cgroup: %w", err)
	}
	info, err := dir.Stat()
	if err != nil {
		_ = dir.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("stat private run cgroup: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		_ = dir.Close()
		_ = os.Remove(path)
		return nil, errors.New("kernel did not provide a usable cgroup v2 ID")
	}
	if _, err := os.Stat(filepath.Join(path, "cgroup.kill")); err != nil {
		_ = dir.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("block mode requires cgroup.kill support: %w", err)
	}
	return &runCgroup{path: path, dir: dir, id: stat.Ino}, nil
}

func (c *runCgroup) ID() uint64 {
	if c == nil {
		return 0
	}
	return c.id
}

func (c *runCgroup) FD() int {
	if c == nil || c.dir == nil {
		return -1
	}
	return int(c.dir.Fd())
}

func (c *runCgroup) Kill() error {
	if c == nil {
		return errors.New("nil run cgroup")
	}
	file, err := os.OpenFile(filepath.Join(c.path, "cgroup.kill"), os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open run cgroup kill control: %w", err)
	}
	_, writeErr := file.Write([]byte("1\n"))
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("kill run cgroup: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(c.path, "cgroup.events"))
		if err != nil {
			return fmt.Errorf("read run cgroup events: %w", err)
		}
		if cgroupEmpty(string(data)) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("run cgroup remained populated after kill")
}

func (c *runCgroup) Close() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.dir != nil {
		if err := c.dir.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close run cgroup directory: %w", err))
		}
		c.dir = nil
	}
	if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("remove run cgroup: %w", err))
	}
	return errors.Join(errs...)
}

func cgroupEmpty(events string) bool {
	for _, line := range strings.Split(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			value, err := strconv.Atoi(fields[1])
			return err == nil && value == 0
		}
	}
	return false
}
