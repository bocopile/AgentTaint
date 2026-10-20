//go:build linux

package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
	"golang.org/x/sys/unix"
)

func TestRunObservedSeparatesConcurrentRuns(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-observed-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	probeFD, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	highFD, err := unix.FcntlInt(uintptr(probeFD), unix.F_DUPFD, 200)
	_ = unix.Close(probeFD)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(highFD)
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	if os.Geteuid() == 0 {
		identity = &TargetIdentity{UID: 65534, GID: 65534}
	}
	groupCheck := ""
	if os.Geteuid() == 0 {
		groupCheck = "[ -z \"$groups\" ] || exit 74\n"
	}
	type result struct {
		index int
		code  int
		out   []byte
		diag  []byte
		err   error
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)
	for index := 0; index < 2; index++ {
		index := index
		fixture := filepath.Join(root, fmt.Sprintf("fixture-%d.txt", index))
		if err := os.WriteFile(fixture, []byte("synthetic\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(root, fmt.Sprintf("target-%d", index))
		source := "#!/bin/sh\nruid= euid= suid= rgid= egid= sgid= nnp= groups= cape= capp= capi= capa=\nwhile IFS=: read -r field value; do\n  case \"$field\" in\n    Uid) set -- $value; ruid=$1; euid=$2; suid=$3;;\n    Gid) set -- $value; rgid=$1; egid=$2; sgid=$3;;\n    NoNewPrivs) set -- $value; nnp=$1;;\n    Groups) set -- $value; groups=$*;;\n    CapEff) set -- $value; cape=$1;;\n    CapPrm) set -- $value; capp=$1;;\n    CapInh) set -- $value; capi=$1;;\n    CapAmb) set -- $value; capa=$1;;\n  esac\ndone < /proc/self/status\n[ \"$ruid:$euid:$suid\" = \"" + fmt.Sprint(identity.UID) + ":" + fmt.Sprint(identity.UID) + ":" + fmt.Sprint(identity.UID) + "\" ] || exit 71\n[ \"$rgid:$egid:$sgid\" = \"" + fmt.Sprint(identity.GID) + ":" + fmt.Sprint(identity.GID) + ":" + fmt.Sprint(identity.GID) + "\" ] || exit 72\n[ \"$nnp\" = 1 ] || exit 73\n" + groupCheck + "case \"$cape:$capp:$capi:$capa\" in *[!0:]* ) exit 75;; esac\n[ ! -e /proc/self/fd/" + fmt.Sprint(highFD) + " ] || exit 79\nwhile IFS= read -r line; do :; done < " + shellQuote(fixture) + "\nprintf 'target-" + fmt.Sprint(index) + "'\nprintf 'diagnostic-" + fmt.Sprint(index) + "' >&2\n"
		if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
			t.Fatal(err)
		}
		auditPath := filepath.Join(root, fmt.Sprintf("audit-%d.jsonl", index))
		go func() {
			start.Wait()
			var out, diagnostic bytes.Buffer
			code, err := RunObserved(context.Background(), env.Resolver{}, script, []string{script}, nil, &out, &diagnostic, auditPath, identity)
			results <- result{index: index, code: code, out: out.Bytes(), diag: diagnostic.Bytes(), err: err}
		}()
	}
	start.Done()
	for range 2 {
		got := <-results
		if got.err != nil || got.code != 0 {
			t.Fatalf("run %d: code=%d err=%v", got.index, got.code, got.err)
		}
		if string(got.out) != fmt.Sprintf("target-%d", got.index) {
			t.Fatalf("run %d target stdout changed: %q", got.index, got.out)
		}
		if string(got.diag) != fmt.Sprintf("diagnostic-%d", got.index) {
			t.Fatalf("run %d target stderr changed: %q", got.index, got.diag)
		}
		fixture := filepath.Join(root, fmt.Sprintf("fixture-%d.txt", got.index))
		other := filepath.Join(root, fmt.Sprintf("fixture-%d.txt", 1-got.index))
		data, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("audit-%d.jsonl", got.index)))
		if err != nil {
			t.Fatal(err)
		}
		if mode, err := os.Stat(filepath.Join(root, fmt.Sprintf("audit-%d.jsonl", got.index))); err != nil || mode.Mode().Perm() != 0o600 {
			t.Fatalf("run %d audit mode: %v %v", got.index, mode, err)
		}
		foundFixture := false
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			event, err := core.DecodeEvent(line)
			if err != nil {
				t.Fatalf("run %d invalid audit line %q: %v", got.index, line, err)
			}
			var context core.ProcessContext
			switch event := event.(type) {
			case *core.ProcessExecEvent:
				context = event.Process
			case *core.ProcessExitEvent:
				context = event.Process
			case *core.FileOpenEvent:
				context = event.Process
				if event.Resource.Path == fixture {
					foundFixture = true
				}
				if event.Resource.Path == other {
					t.Fatalf("run %d audit contains sibling run file %s", got.index, other)
				}
			case *core.ProcessForkEvent:
				context = event.Process
			}
			if context.ProcessKey.PID == 0 || context.ProcessKey.ScopeID == "" {
				t.Fatalf("run %d event lacks process scope: %#v", got.index, event)
			}
		}
		if !foundFixture {
			t.Fatalf("run %d audit lacks its synthetic file-open event: %s", got.index, strings.TrimSpace(string(data)))
		}
	}
}

func TestRunObservedPolicyAuditPipeline(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-policy-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	normal := filepath.Join(root, "normal.txt")
	for path, content := range map[string]string{secret: "synthetic secret\n", normal: "ordinary data\n"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan struct{}, 4)
	go func() {
		for range 4 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	run := func(name, action, body string) []*core.PolicyViolationEvent {
		t.Helper()
		policyPath := filepath.Join(root, name+".yaml")
		policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: " + name + "\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: " + action + "\n"
		if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := policy.Load(policyPath)
		if err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(root, name+".sh")
		scriptText := "#!/bin/bash\nset -e\n" + body + "\n"
		if err := os.WriteFile(script, []byte(scriptText), 0o755); err != nil {
			t.Fatal(err)
		}
		auditPath := filepath.Join(root, name+".jsonl")
		code, err := RunObservedWithPolicy(context.Background(), env.Resolver{}, script, []string{script}, nil, io.Discard, io.Discard, auditPath, snapshot, identity)
		if err != nil || code != 0 {
			t.Fatalf("%s run: code=%d err=%v", name, code, err)
		}
		data, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatal(err)
		}
		var violations []*core.PolicyViolationEvent
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			event, err := core.DecodeEvent(line)
			if err != nil {
				t.Fatalf("%s invalid audit event %s: %v", name, line, err)
			}
			if violation, ok := event.(*core.PolicyViolationEvent); ok {
				violations = append(violations, violation)
			}
		}
		return violations
	}

	connect := "exec 4<>/dev/tcp/127.0.0.1/" + fmt.Sprint(port)
	if got := run("normal", "deny", "exec 3<"+shellQuote(normal)+"\n/bin/bash -c "+shellQuote(connect)); len(got) != 0 {
		t.Fatalf("normal source flow was tainted: %#v", got)
	}
	if got := run("sensitive-read", "deny", "exec 3<"+shellQuote(secret)+"\n"+connect); len(got) != 1 || got[0].Decision != "deny" || got[0].OperationResult != core.ResultSuccess {
		t.Fatalf("direct deny did not preserve successful operation: %#v", got)
	}
	if got := run("subprocess-exfil", "deny", "exec 3<"+shellQuote(secret)+"\n/bin/bash -c "+shellQuote(connect)); len(got) != 1 || got[0].Decision != "deny" || got[0].OperationResult != core.ResultSuccess {
		t.Fatalf("child sink was not audited: %#v", got)
	}
	if got := run("audit-only", "audit", "exec 3<"+shellQuote(secret)+"\n"+connect); len(got) != 1 || got[0].Decision != "audit" || got[0].OperationResult != core.ResultSuccess {
		t.Fatalf("audit-only flow changed operation result: %#v", got)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-accepted:
		case <-time.After(2 * time.Second):
			t.Fatal("the deny/audit verdict prevented a local TCP connection")
		}
	}
}

func TestRunObservedNoNewPrivilegesBlocksSetuidTarget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to set up a setuid fixture")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-setuid-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Flags&unix.ST_NOSUID != 0 {
		t.Skipf("temporary filesystem is mounted nosuid (flags=%#x mask=%#x)", fs.Flags, unix.ST_NOSUID)
	}
	idBinary, err := os.ReadFile("/usr/bin/id")
	if err != nil {
		t.Fatal(err)
	}
	setuidBinary := filepath.Join(root, "id")
	if err := os.WriteFile(setuidBinary, idBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(setuidBinary, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(setuidBinary, 0o4755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(setuidBinary)
	if err != nil || info.Mode()&os.ModeSetuid == 0 {
		t.Fatalf("setuid fixture is not active: %v %v", info, err)
	}
	var out, diagnostic bytes.Buffer
	identity := &TargetIdentity{UID: 1000, GID: 1000}
	code, runErr := RunObserved(context.Background(), env.Resolver{}, setuidBinary, []string{setuidBinary, "-u"}, nil, &out, &diagnostic, filepath.Join(root, "audit.jsonl"), identity)
	if code != 0 || runErr != nil || strings.TrimSpace(out.String()) != "65534" {
		t.Fatalf("setuid target regained privileges: code=%d err=%v stdout=%q stderr=%q", code, runErr, out.String(), diagnostic.String())
	}
}

func TestRunObservedForwardsCancellationToTargetGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-cancel-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("/tmp", fmt.Sprintf("agenttaint-target-started-%d", os.Getpid()))
	_ = os.Remove(marker)
	defer os.Remove(marker)
	script := filepath.Join(root, "target")
	source := "#!/bin/sh\n: > " + shellQuote(marker) + "\nexec /bin/sleep 30\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := RunObserved(ctx, env.Resolver{}, script, []string{script}, nil, io.Discard, io.Discard, filepath.Join(root, "audit.jsonl"), &TargetIdentity{UID: 65534, GID: 65534})
		result <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("target did not start before cancellation deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case got := <-result:
		if got.code != 130 || got.err == nil || !strings.Contains(got.err.Error(), "run-canceled") {
			t.Fatalf("canceled target result: code=%d err=%v", got.code, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("target group did not terminate after cancellation")
	}
}

func TestRunObservedKeepsObserverUntilSetsidDescendantExits(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("requires util-linux setsid")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-descendant-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("/tmp", fmt.Sprintf("agenttaint-descendant-exit-%d", os.Getpid()))
	_ = os.Remove(marker)
	defer os.Remove(marker)
	script := filepath.Join(root, "target")
	source := "#!/bin/sh\n" + shellQuote(setsid) + " /bin/sh -c 'sleep 5.2; : > " + shellQuote(marker) + "' </dev/null >/dev/null 2>&1 &\nexit 0\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	code, runErr := RunObserved(context.Background(), env.Resolver{}, script, []string{script}, nil, io.Discard, io.Discard, filepath.Join(root, "audit.jsonl"), &TargetIdentity{UID: 65534, GID: 65534})
	if elapsed := time.Since(started); elapsed < childDrainGrace {
		t.Fatalf("observer detached before descendant drain limit: elapsed=%s", elapsed)
	}
	if code != 1 || runErr == nil || !strings.Contains(runErr.Error(), "descendant-remained-after-drain-limit") {
		t.Fatalf("long-lived descendant result: code=%d err=%v", code, runErr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("observer returned while setsid descendant remained alive: marker=%v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestTargetCredentialRejectsRootAndUnknownIdentity(t *testing.T) {
	if os.Geteuid() == 0 {
		if _, _, err := targetCredential(nil); err == nil {
			t.Fatal("root observer accepted an unspecified target identity")
		}
		if _, _, err := targetCredential(&TargetIdentity{UID: 0, GID: 0}); err == nil {
			t.Fatal("root observer accepted a root target")
		}
		credential, identity, err := targetCredential(&TargetIdentity{UID: 65534, GID: 65534})
		if err != nil || credential == nil || credential.Uid != 65534 || credential.Gid != 65534 || len(credential.Groups) != 0 {
			t.Fatalf("explicit unprivileged target identity: %#v %v", credential, err)
		}
		if identity != (TargetIdentity{UID: 65534, GID: 65534}) {
			t.Fatalf("normalized identity: %#v", identity)
		}
		return
	}
	if credential, identity, err := targetCredential(nil); err != nil || credential != nil || identity.UID != uint32(os.Geteuid()) || identity.GID != uint32(os.Getegid()) {
		t.Fatalf("unprivileged caller identity: %#v %v", credential, err)
	}
	if _, _, err := targetCredential(&TargetIdentity{UID: 0, GID: 0}); err == nil {
		t.Fatal("unprivileged observer accepted identity escalation")
	}
}

func TestObserveHelperDoesNotExecWithoutExactPermit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"eof", "short", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "target-started")
			script := filepath.Join(root, "target")
			if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			parentFD, childFD := fds[0], fds[1]
			child := os.NewFile(uintptr(childFD), "test-helper-control")
			nonce := strings.Repeat("a", 32)
			cmd := exec.Command(executable, observeHelperArg, script, fmt.Sprint(os.Geteuid()), fmt.Sprint(os.Getegid()), "false", "target")
			cmd.ExtraFiles = []*os.File{child}
			cmd.Env = helperEnvironment(os.Environ(), nonce)
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			if err := cmd.Start(); err != nil {
				_ = child.Close()
				_ = unix.Close(parentFD)
				t.Fatal(err)
			}
			_ = child.Close()
			if err := waitControlFrame(context.Background(), parentFD, []byte("READY"), time.Second); err != nil {
				_ = unix.Close(parentFD)
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal(err)
			}
			if mode == "short" {
				_, _ = unix.Write(parentFD, []byte("S"))
			} else if mode == "invalid" {
				_, _ = unix.Write(parentFD, []byte("FAIL:"+nonce))
			}
			_ = unix.Close(parentFD)
			if err := cmd.Wait(); err == nil {
				t.Fatal("helper accepted missing or invalid launch permit")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("target ran without a valid permit: stat=%v stderr=%q", err, diagnostic.String())
			}
		})
	}
}

func TestStartObserveHelperRejectsCanceledContextBeforeChildStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	marker := filepath.Join(t.TempDir(), "target-started")
	cmd, fd, _, err := startObserveHelper(ctx, "/bin/sh", []string{"/bin/sh", "-c", "touch " + shellQuote(marker)}, nil, nil, nil, nil, TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())})
	if err == nil || cmd != nil || fd >= 0 {
		t.Fatalf("canceled launch unexpectedly created helper: cmd=%v fd=%d err=%v", cmd, fd, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("target started before launch permit: %v", statErr)
	}
}
