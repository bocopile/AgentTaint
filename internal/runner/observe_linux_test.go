//go:build linux

package runner

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/bocopile/AgentTaint/internal/enforce"
	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
	"golang.org/x/sys/unix"
)

const (
	supervisorCrashFixtureEnv   = "AGENTTAINT_SUPERVISOR_CRASH_FIXTURE"
	supervisorCrashPolicyEnv    = "AGENTTAINT_SUPERVISOR_CRASH_POLICY"
	supervisorCrashTargetEnv    = "AGENTTAINT_SUPERVISOR_CRASH_TARGET"
	supervisorCrashScopePathEnv = "AGENTTAINT_SUPERVISOR_CRASH_SCOPE_PATH"
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

func TestRunPolicyModesNotifyAndKillAfterPolicyEvent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load eBPF programs and create the kill-mode run cgroup")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-modes-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan error, 3)
	go func() {
		for range 3 {
			conn, err := listener.Accept()
			if err != nil {
				accepted <- err
				return
			}
			_ = conn.Close()
			accepted <- nil
		}
	}()
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: post-event-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	connect := "exec 3<" + shellQuote(secret) + "\nexec 4<>/dev/tcp/127.0.0.1/" + fmt.Sprint(port) + "\nsleep 1\nprintf survived"
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	for _, mode := range []PolicyMode{ModeMonitor, ModeNotify, ModeKill} {
		if mode == ModeKill {
			probe, probeErr := createRunCgroup()
			if probeErr != nil {
				marker := filepath.Join(root, "kill-mode-started")
				preflight := filepath.Join(root, "kill-preflight.sh")
				if err := os.WriteFile(preflight, []byte("#!/bin/sh\nprintf started > "+shellQuote(marker)+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, preflight, []string{preflight}, nil, &out, &diagnostic, filepath.Join(root, "kill-preflight.jsonl"), snapshot, identity, ModeKill)
				if runErr == nil || !strings.Contains(runErr.Error(), "prepare run cgroup scope") || status.ActiveMode != "none" || status.Result != "not_started" || code == 0 {
					t.Fatalf("kill mode did not fail safely before start when cgroup scope is unavailable: code=%d status=%+v stderr=%q err=%v", code, status, diagnostic.String(), runErr)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("kill target ran without a private cgroup scope: marker stat err=%v", err)
				}
				t.Logf("kill mode failed before target start because private cgroup scope is unavailable: %v", probeErr)
				return
			}
			if err := probe.Close(); err != nil {
				t.Fatalf("close kill-mode cgroup preflight probe: %v", err)
			}
		}
		script := filepath.Join(root, string(mode)+".sh")
		if err := os.WriteFile(script, []byte("#!/bin/bash\nset -e\n"+connect+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, script, []string{script}, nil, &stdout, &stderr, filepath.Join(root, string(mode)+".jsonl"), snapshot, identity, mode)
		if status.RequestedMode != mode || status.ActiveMode != mode {
			t.Fatalf("%s mode status does not report the active run: %+v", mode, status)
		}
		if status.AuditCoverage == "incomplete" {
			if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
				t.Fatalf("%s mode did not report incomplete audit coverage: status=%+v err=%v", mode, status, runErr)
			}
			t.Logf("%s mode action completed with audit coverage reported incomplete: code=%d err=%v", mode, code, runErr)
		} else if status.AuditCoverage != "complete" || runErr != nil {
			t.Fatalf("%s mode returned unexpected coverage/result: status=%+v err=%v", mode, status, runErr)
		}
		switch mode {
		case ModeMonitor:
			if status.ActualAction != "observe_only" || status.Notifications != 0 || !strings.Contains(stdout.String(), "survived") {
				t.Fatalf("monitor changed the target or reported an action: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		case ModeNotify:
			if status.ActualAction != "post_event_notification" || status.Notifications != 1 || !strings.Contains(stderr.String(), "audit_event_id=") || !strings.Contains(stdout.String(), "survived") {
				t.Fatalf("notify did not report its linked post-event action: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		case ModeKill:
			if status.ActualAction != "post_event_kill" || status.Result != "cgroup_killed_after_policy_event" || strings.Contains(stdout.String(), "survived") {
				t.Fatalf("kill did not terminate the run after the policy event: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		}
		data, err := os.ReadFile(filepath.Join(root, string(mode)+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		var foundDeny bool
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			event, err := core.DecodeEvent(line)
			if err != nil {
				t.Fatalf("%s invalid audit event %s: %v", mode, line, err)
			}
			if violation, ok := event.(*core.PolicyViolationEvent); ok && violation.Decision == "deny" && violation.OperationResult == core.ResultSuccess {
				foundDeny = true
				if mode == ModeNotify && !strings.Contains(stderr.String(), "audit_event_id="+string(violation.EventID)) {
					t.Fatalf("notification is not linked to audit event %s: %q", violation.EventID, stderr.String())
				}
			}
		}
		if !foundDeny {
			t.Fatalf("%s mode lacks a successful post-event deny record", mode)
		}
		select {
		case acceptErr := <-accepted:
			if acceptErr != nil {
				t.Fatalf("%s mode receiver did not confirm the connection effect: %v", mode, acceptErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s mode connection did not reach the controlled receiver", mode)
		}
	}
}

func TestRunPolicyModeBlockReportsPreOperationEnforcement(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load eBPF programs and create the block-mode run cgroup")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-mode-block-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: block-mode-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(blockChildEnv, "1")
	t.Setenv(blockSourceEnv, secret)
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockExpectEnv, "deny")
	target := os.Args[0]
	var stdout, stderr bytes.Buffer
	code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, target, []string{target, "-test.run=^TestKernelBlockChild$"}, nil, &stdout, &stderr, filepath.Join(root, "block.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock)
	if status.RequestedMode != ModeBlock || status.ActiveMode != ModeBlock || status.ActualAction != "pre_op_lsm_enforcement_active" {
		t.Fatalf("block status does not identify active pre-operation enforcement: code=%d status=%+v stderr=%q err=%v", code, status, stderr.String(), runErr)
	}
	if !strings.Contains(stdout.String(), "open_errno=0 connect_errno=1") {
		t.Fatalf("block mode did not preserve the source open and pre-operation EPERM result: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
	}
	if status.AuditCoverage == "incomplete" {
		if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
			t.Fatalf("block mode did not report incomplete audit coverage: status=%+v err=%v", status, runErr)
		}
	} else if status.AuditCoverage != "complete" || runErr != nil {
		t.Fatalf("block mode returned unexpected coverage/result: status=%+v err=%v", status, runErr)
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted the connect denied by block mode")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("block-mode receiver check: %v", acceptErr)
	}
}

func TestRunPolicyModeScenarioMatrix(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load eBPF programs and create private run cgroups")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-mode-matrix-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	normal := filepath.Join(root, "normal.txt")
	for path, data := range map[string]string{secret: "synthetic secret\n", normal: "ordinary data\n"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	type scenario struct {
		name       string
		source     string
		forkedSink bool
	}
	scenarios := []scenario{
		{name: "normal", source: normal},
		{name: "direct-source", source: secret},
		{name: "fork-child", source: secret, forkedSink: true},
		{name: "audit-only", source: secret},
	}
	modes := []PolicyMode{ModeMonitor, ModeNotify, ModeBlock, ModeKill}
	for _, scenario := range scenarios {
		for _, mode := range modes {
			t.Run(string(mode)+"/"+scenario.name, func(t *testing.T) {
				action := "deny"
				if scenario.name == "audit-only" {
					action = "audit"
				}
				policyPath := filepath.Join(root, string(mode)+"-"+scenario.name+".yaml")
				policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: mode-matrix\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: %s\n", secret, action)
				if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
					t.Fatal(err)
				}
				snapshot, err := policy.Load(policyPath)
				if err != nil {
					t.Fatal(err)
				}
				connect := "exec 4<>/dev/tcp/127.0.0.1/" + fmt.Sprint(port)
				var sink string
				if scenario.forkedSink {
					sink = "if /bin/bash -c " + shellQuote(connect) + "; then printf child_connected; else printf child_connect_failed; fi"
				} else {
					sink = "if " + connect + "; then printf connected; else printf connect_failed; fi"
				}
				script := filepath.Join(root, string(mode)+"-"+scenario.name+".sh")
				scriptBody := "#!/bin/bash\nset +e\nexec 3<" + shellQuote(scenario.source) + "\n" + sink + "\nsleep 0.2\nprintf survived\n"
				if scenario.name == "audit-only" {
					scriptBody = strings.Replace(scriptBody, "printf survived", "printf audited", 1)
				}
				if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				auditPath := filepath.Join(root, string(mode)+"-"+scenario.name+".jsonl")
				code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, script, []string{script}, nil, &stdout, &stderr, auditPath, snapshot, identity, mode)
				if status.RequestedMode != mode || status.ActiveMode != mode {
					t.Fatalf("mode status does not report the active run: code=%d status=%+v stderr=%q err=%v", code, status, stderr.String(), runErr)
				}
				if status.AuditCoverage == "incomplete" {
					if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
						t.Fatalf("incomplete audit coverage was not surfaced: status=%+v err=%v", status, runErr)
					}
				} else if status.AuditCoverage != "complete" || runErr != nil {
					t.Fatalf("unexpected audit coverage result: status=%+v err=%v", status, runErr)
				}
				tainted := scenario.name != "normal"
				denied := tainted && action == "deny"
				blocked := mode == ModeBlock && denied
				killed := mode == ModeKill && denied
				connected := !blocked
				if blocked {
					if scenario.forkedSink {
						if !strings.Contains(stdout.String(), "child_connect_failed") {
							t.Fatalf("fork child connect was not rejected: stdout=%q stderr=%q", stdout.String(), stderr.String())
						}
					} else if !strings.Contains(stdout.String(), "connect_failed") {
						t.Fatalf("direct connect was not rejected: stdout=%q stderr=%q", stdout.String(), stderr.String())
					}
				} else if !strings.Contains(stdout.String(), "connected") {
					t.Fatalf("expected target connection to complete: stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
				continued := strings.Contains(stdout.String(), "survived") || strings.Contains(stdout.String(), "audited")
				if killed == continued {
					t.Fatalf("kill result does not match the target's post-event continuation: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
				}
				if scenario.name == "audit-only" && !strings.Contains(stdout.String(), "audited") {
					t.Fatalf("audit-only policy changed target continuation: stdout=%q", stdout.String())
				}
				switch mode {
				case ModeMonitor:
					if status.ActualAction != "observe_only" || status.Notifications != 0 {
						t.Fatalf("monitor reported an enforcement action: %+v", status)
					}
				case ModeNotify:
					expectedNotifications := uint64(0)
					if tainted {
						expectedNotifications = 1
					}
					if status.Notifications != expectedNotifications {
						t.Fatalf("notify count=%d, want %d: stderr=%q", status.Notifications, expectedNotifications, stderr.String())
					}
					if expectedNotifications == 1 && (!strings.Contains(status.ActualAction, "notification") || !strings.Contains(stderr.String(), "audit_event_id=")) {
						t.Fatalf("notify did not reference an audit event: status=%+v stderr=%q", status, stderr.String())
					}
				case ModeBlock:
					if status.ActualAction != "pre_op_lsm_enforcement_active" {
						t.Fatalf("block did not report pre-operation enforcement: %+v", status)
					}
				case ModeKill:
					if killed && (status.ActualAction != "post_event_kill" || status.Result != "cgroup_killed_after_policy_event") {
						t.Fatalf("explicit kill did not report its post-event result: %+v", status)
					}
					if !killed && status.ActualAction != "none" {
						t.Fatalf("kill mode acted without a deny event: %+v", status)
					}
				}
				if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				conn, acceptErr := listener.Accept()
				_ = listener.(*net.TCPListener).SetDeadline(time.Time{})
				if connected {
					if acceptErr != nil {
						t.Fatalf("expected connection did not reach receiver: %v", acceptErr)
					}
					_ = conn.Close()
				} else {
					if acceptErr == nil {
						_ = conn.Close()
						t.Fatal("block mode allowed a denied connection to reach the receiver")
					}
					if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
						t.Fatalf("receiver block check returned %v", acceptErr)
					}
				}
			})
		}
	}
}

func TestRunEnforcementModesFailBeforeTargetWithoutRunScopePrivilege(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged Linux caller to verify run-scope preflight failure")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-mode-preflight-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: preflight-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	identity := &TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	for _, mode := range []PolicyMode{ModeBlock, ModeKill} {
		marker := filepath.Join(root, string(mode)+"-started")
		var stdout, stderr bytes.Buffer
		code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, "/usr/bin/touch", []string{"/usr/bin/touch", marker}, nil, &stdout, &stderr, filepath.Join(root, string(mode)+".jsonl"), snapshot, identity, mode)
		if runErr == nil || !strings.Contains(runErr.Error(), "prepare run cgroup scope") || status.ActiveMode != "none" || status.ActualAction != "none" || status.Result != "not_started" || code == 0 {
			t.Fatalf("%s mode did not fail before target start without private-scope privilege: code=%d status=%+v stdout=%q stderr=%q err=%v", mode, code, status, stdout.String(), stderr.String(), runErr)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("%s mode started the target without private-scope privilege: marker stat err=%v", mode, err)
		}
	}
}

func TestRunBlockHookPreparationFailureDoesNotStartTarget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to verify block hook preparation failure")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-hook-preflight-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: hook-preflight-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	scopePath := filepath.Join(root, "synthetic-cgroup")
	createScope := func() (*runCgroup, error) {
		if err := os.Mkdir(scopePath, 0o700); err != nil {
			return nil, err
		}
		return &runCgroup{path: scopePath, id: 1}, nil
	}
	newEnforcer := func(_ policy.KernelProjection, _ uint64) (*enforce.Enforcer, error) {
		return nil, errors.New("synthetic required socket_connect hook attach failure")
	}
	marker := filepath.Join(root, "target-started")
	auditPath := filepath.Join(root, "audit.jsonl")
	var stdout, stderr bytes.Buffer
	status := ModeStatus{RequestedMode: ModeBlock, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	code, runErr := runProtectedPlatformWithDependencies(context.Background(), "/usr/bin/touch", []string{"/usr/bin/touch", marker}, nil, &stdout, &stderr, auditPath, snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock, &status, createScope, newEnforcer)
	if code == 0 || runErr == nil || !strings.Contains(runErr.Error(), "prepare kernel enforcement hooks") || !strings.Contains(runErr.Error(), "synthetic required socket_connect hook attach failure") {
		t.Fatalf("block did not report the hook preparation failure: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
	}
	if status.ActiveMode != "none" || status.ActualAction != "none" || status.Result != "not_started" {
		t.Fatalf("failed block preflight reported an active mode or action: %+v", status)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("target started after the required hook failed to attach: marker stat err=%v", err)
	}
	if _, err := os.Stat(auditPath); !os.IsNotExist(err) {
		t.Fatalf("audit output was created before block hook preflight completed: stat err=%v", err)
	}
	if _, err := os.Stat(scopePath); !os.IsNotExist(err) {
		t.Fatalf("synthetic run scope was not cleaned up after preflight failure: stat err=%v", err)
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

func TestRunKillModeCleansScopeAfterDetachedDescendantDrainLimit(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to create and kill the protected run cgroup")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("requires util-linux setsid")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-kill-descendant-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: detached-descendant\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	readyFIFO := filepath.Join(root, "descendant-ready")
	if err := unix.Mkfifo(readyFIFO, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readyFIFO, 0o666); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "descendant-survived")
	script := filepath.Join(root, "target")
	body := fmt.Sprintf("#!/bin/sh\n%s /bin/sh -c 'printf ready > %s; /bin/sleep 30; : > %s' </dev/null >/dev/null 2>&1 &\nIFS= read -r _ < %s\nexit 0\n", shellQuote(setsid), shellQuote(readyFIFO), shellQuote(marker), shellQuote(readyFIFO))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	var scopePath string
	createScope := func() (*runCgroup, error) {
		scope, err := createRunCgroup()
		if err == nil {
			scopePath = scope.path
		}
		return scope, err
	}
	status := ModeStatus{RequestedMode: ModeKill, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	started := time.Now()
	code, runErr := runProtectedPlatformWithDependencies(context.Background(), script, []string{script}, nil, io.Discard, io.Discard, filepath.Join(root, "audit.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeKill, &status, createScope, enforce.New)
	if time.Since(started) < childDrainGrace {
		t.Fatalf("runner detached before its descendant drain limit: elapsed=%s status=%+v err=%v", time.Since(started), status, runErr)
	}
	if runErr == nil || !strings.Contains(runErr.Error(), "descendant-remained-after-drain-limit") || code == 0 {
		t.Fatalf("detached descendant did not produce the configured drain-limit result: code=%d status=%+v err=%v", code, status, runErr)
	}
	if scopePath == "" {
		t.Fatal("run cgroup was not created")
	}
	if _, err := os.Stat(scopePath); !os.IsNotExist(err) {
		t.Fatalf("run cgroup scope was not removed after descendant cleanup: path=%q stat err=%v", scopePath, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("detached descendant survived run-scope cleanup: marker stat err=%v", err)
	}
	t.Logf("kill mode drain-limit cleanup: elapsed=%s cgroup_removed=true descendant_marker_absent=true status=%+v", time.Since(started), status)
}

// TestRunKillModeSupervisorCrashChild is launched by the companion test and
// deliberately left running until the parent process kills this supervisor.
func TestRunKillModeSupervisorCrashChild(t *testing.T) {
	if os.Getenv(supervisorCrashFixtureEnv) != "1" {
		return
	}
	snapshot, err := policy.Load(os.Getenv(supervisorCrashPolicyEnv))
	if err != nil {
		t.Fatal(err)
	}
	var scopePath string
	createScope := func() (*runCgroup, error) {
		scope, err := createRunCgroup()
		if err != nil {
			return nil, err
		}
		scopePath = scope.path
		if err := os.WriteFile(os.Getenv(supervisorCrashScopePathEnv), []byte(scopePath), 0o600); err != nil {
			_ = scope.Kill()
			_ = scope.Close()
			return nil, err
		}
		return scope, nil
	}
	var stdout, stderr bytes.Buffer
	status := ModeStatus{RequestedMode: ModeKill, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	code, runErr := runProtectedPlatformWithDependencies(context.Background(), os.Getenv(supervisorCrashTargetEnv), []string{os.Getenv(supervisorCrashTargetEnv)}, nil, &stdout, &stderr, os.Getenv(supervisorCrashPolicyEnv)+".audit", snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeKill, &status, createScope, enforce.New)
	t.Errorf("supervisor fixture exited before the test killed it: code=%d status=%+v err=%v stdout=%q stderr=%q scope=%q", code, status, runErr, stdout.String(), stderr.String(), scopePath)
}

func TestRunKillModeSupervisorCrashLeavesDescendantScopeForExternalCleanup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to inspect and clean the protected cgroup after supervisor death")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("requires util-linux setsid")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-supervisor-crash-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: supervisor-crash\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	targetStarted := filepath.Join(root, "target-started")
	childStarted := filepath.Join(root, "child-started")
	for _, path := range []string{targetStarted, childStarted} {
		if err := os.WriteFile(path, []byte("not-started"), 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "target")
	child := "printf child-started > " + shellQuote(childStarted) + "; exec /bin/sleep 30"
	body := fmt.Sprintf("#!/bin/sh\nprintf target-started > %s\n%s /bin/sh -c %s </dev/null >/dev/null 2>&1 &\nwait\n", shellQuote(targetStarted), shellQuote(setsid), shellQuote(child))
	if err := os.WriteFile(target, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	scopePathFile := filepath.Join(root, "scope-path")
	var stdout, stderr bytes.Buffer
	supervisor := exec.Command(os.Args[0], "-test.run=^TestRunKillModeSupervisorCrashChild$")
	supervisor.Env = append(os.Environ(),
		supervisorCrashFixtureEnv+"=1",
		supervisorCrashPolicyEnv+"="+policyPath,
		supervisorCrashTargetEnv+"="+target,
		supervisorCrashScopePathEnv+"="+scopePathFile,
	)
	supervisor.Stdout, supervisor.Stderr = &stdout, &stderr
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	scopePath := ""
	defer func() {
		if supervisor.ProcessState == nil {
			_ = supervisor.Process.Kill()
			_ = supervisor.Wait()
		}
		if scopePath != "" {
			if scope, err := openRunCgroup(scopePath); err == nil {
				_ = scope.Kill()
				_ = scope.Close()
			}
		}
	}()
	waitForContent := func(path, expected string, timeout time.Duration) error {
		t.Helper()
		deadline := time.Now().Add(timeout)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if data, err := os.ReadFile(path); err == nil {
				got := strings.TrimSpace(string(data))
				if (expected == "" && got != "") || (expected != "" && got == expected) {
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for %s to contain %q", path, expected)
			}
			<-ticker.C
		}
	}
	if err := waitForContent(scopePathFile, "", 5*time.Second); err != nil {
		t.Fatalf("run scope was not created: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(scopePathFile)
	if err != nil {
		t.Fatalf("read private run cgroup path: %v", err)
	}
	scopePath = strings.TrimSpace(string(data))
	if scopePath == "" {
		t.Fatal("supervisor recorded an empty run cgroup path")
	}
	if err := waitForContent(targetStarted, "target-started", 5*time.Second); err != nil {
		t.Fatalf("target did not start: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	if err := waitForContent(childStarted, "child-started", 5*time.Second); err != nil {
		t.Fatalf("detached child did not start: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	if err := supervisor.Process.Kill(); err != nil {
		t.Fatalf("kill test-owned supervisor: %v", err)
	}
	if err := supervisor.Wait(); err == nil {
		t.Fatal("SIGKILL unexpectedly returned a successful supervisor exit")
	}
	if _, err := os.Stat(scopePath); err != nil {
		t.Fatalf("supervisor crash unexpectedly removed the cgroup before external cleanup: %v", err)
	}
	eventsPath := filepath.Join(scopePath, "cgroup.events")
	events, err := os.ReadFile(eventsPath)
	if err != nil || !strings.Contains(string(events), "populated 1") {
		t.Fatalf("expected the detached child to keep the abandoned run cgroup populated: events=%q err=%v", events, err)
	}
	scope, err := openRunCgroup(scopePath)
	if err != nil {
		t.Fatalf("open abandoned test-owned scope for cleanup: %v", err)
	}
	cleanupErr := errors.Join(scope.Kill(), scope.Close())
	if cleanupErr != nil {
		t.Fatalf("externally clean abandoned run scope: %v", cleanupErr)
	}
	if _, err := os.Stat(scopePath); !os.IsNotExist(err) {
		t.Fatalf("external cleanup did not remove abandoned scope: %v", err)
	}
	t.Logf("supervisor SIGKILL left detached descendant scope populated until explicit external cgroup.kill; test cleanup removed it")
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
