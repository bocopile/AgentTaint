//go:build linux

package runner

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	observeHelperEnv = "AGENTTAINT_INTERNAL_OBSERVE_HELPER"
	observeHelperArg = "--agenttaint-internal-observe-helper"
	observeControlFD = 3
)

func init() {
	if nonce := os.Getenv(observeHelperEnv); nonce != "" {
		os.Exit(runObserveTargetHelper(nonce))
	}
}

func runObserveTargetHelper(nonce string) int {
	if len(nonce) != 32 || len(os.Args) < 7 || os.Args[1] != observeHelperArg || os.Args[2] == "" {
		return helperFailure("invalid internal target helper arguments")
	}
	uid, err := strconv.ParseUint(os.Args[3], 10, 32)
	if err != nil {
		return helperFailure("invalid helper UID")
	}
	gid, err := strconv.ParseUint(os.Args[4], 10, 32)
	if err != nil {
		return helperFailure("invalid helper GID")
	}
	clearGroups := os.Args[5] == "true"
	if os.Args[5] != "true" && os.Args[5] != "false" {
		return helperFailure("invalid helper group policy")
	}
	if err := writeControl([]byte("READY")); err != nil {
		return helperFailure("target helper readiness write failed: " + err.Error())
	}
	start := []byte("START:" + nonce)
	if err := readControlExact(start); err != nil {
		return helperReject("target launch permit missing: "+err.Error(), true)
	}
	if err := validateHelperIdentity(uint32(uid), uint32(gid), clearGroups); err != nil {
		return helperReject("unsafe target helper credentials: "+err.Error(), true)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return helperReject("set no_new_privs: "+err.Error(), true)
	}
	if _, err := unix.FcntlInt(uintptr(observeControlFD), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		return helperReject("close control descriptor on target exec: "+err.Error(), true)
	}
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, observeHelperEnv+"=") {
			env = append(env, entry)
		}
	}
	if err := syscall.Exec(os.Args[2], os.Args[6:], env); err != nil {
		_ = writeControl([]byte("ERROR:" + err.Error()))
		fmt.Fprintln(os.Stderr, "agenttaint: target exec failed:", err)
		return 127
	}
	return 127
}

func validateHelperIdentity(uid, gid uint32, clearGroups bool) error {
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	if uint32(ruid) != uid || uint32(euid) != uid || uint32(suid) != uid || uint32(rgid) != gid || uint32(egid) != gid || uint32(sgid) != gid {
		return fmt.Errorf("real/effective/saved IDs do not match requested identity")
	}
	if clearGroups {
		groups, err := unix.Getgroups()
		if err != nil {
			return fmt.Errorf("read supplementary groups: %w", err)
		}
		if len(groups) != 0 {
			return fmt.Errorf("supplementary groups were not cleared")
		}
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	header := &unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := make([]unix.CapUserData, 2)
	if err := unix.Capget(header, &data[0]); err != nil {
		return fmt.Errorf("read process capabilities: %w", err)
	}
	if data[0].Effective|data[0].Permitted|data[0].Inheritable|data[1].Effective|data[1].Permitted|data[1].Inheritable != 0 {
		zero := make([]unix.CapUserData, 2)
		if err := unix.Capset(header, &zero[0]); err != nil {
			return fmt.Errorf("drop process capabilities: %w", err)
		}
		clear(data)
		if err := unix.Capget(header, &data[0]); err != nil {
			return fmt.Errorf("verify dropped capabilities: %w", err)
		}
		if data[0].Effective|data[0].Permitted|data[0].Inheritable|data[1].Effective|data[1].Permitted|data[1].Inheritable != 0 {
			return fmt.Errorf("process capabilities remain set")
		}
	}
	return nil
}

func helperFailure(message string) int {
	fmt.Fprintln(os.Stderr, "agenttaint: observer launch helper:", message)
	return 125
}

func helperReject(message string, controlReady bool) int {
	if controlReady {
		_ = writeControl([]byte("ERROR:" + message))
	}
	return helperFailure(message)
}

func readControlExact(want []byte) error {
	got := make([]byte, len(want))
	read := 0
	for read < len(got) {
		n, err := unix.Read(observeControlFD, got[read:])
		if err != nil {
			return err
		}
		if n == 0 {
			return syscall.EPIPE
		}
		read += n
	}
	if string(got) != string(want) {
		return fmt.Errorf("invalid launch permit")
	}
	return nil
}

func writeControl(data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(observeControlFD, data)
		if err != nil {
			return err
		}
		if n == 0 {
			return syscall.EPIPE
		}
		data = data[n:]
	}
	return nil
}
