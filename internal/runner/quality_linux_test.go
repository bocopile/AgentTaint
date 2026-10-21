//go:build linux

package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	linuxsensor "github.com/bocopile/AgentTaint/internal/sensor/linux"
)

func TestKernelRingBufferDropRemainsStickyAfterRecovery(t *testing.T) {
	if os.Getenv("AGENTTAINT_RING_DROP_CHILD") == "1" {
		ringDropTargetChild(t)
		return
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" || os.Geteuid() != 0 {
		t.Skip("requires a privileged Linux/arm64 BPF-LSM guest")
	}

	sensor, err := linuxsensor.New(uint64(time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("create Linux sensor: %v", err)
	}
	defer sensor.Close()
	if err := sensor.Barrier(10 * time.Second); err != nil {
		t.Fatalf("drain sensor before ring overflow fixture: %v", err)
	}
	readerGuard := time.AfterFunc(15*time.Second, func() { _ = sensor.Close() })
	defer readerGuard.Stop()

	marker, err := os.CreateTemp("", "agenttaint-ring-recovered-*")
	if err != nil {
		t.Fatal(err)
	}
	markerPath := marker.Name()
	if err := marker.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(markerPath)

	command := exec.Command(os.Args[0], "-test.run=^TestKernelRingBufferDropRemainsStickyAfterRecovery$")
	command.Env = append(os.Environ(), "AGENTTAINT_RING_DROP_CHILD=1", "AGENTTAINT_RING_DROP_MARKER="+markerPath)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil && command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	protocol := bufio.NewReader(stdout)
	readLine := func(want string) {
		t.Helper()
		line, err := protocol.ReadString('\n')
		if err != nil || line != want+"\n" {
			t.Fatalf("child protocol: got %q, err=%v; want %q", line, err, want)
		}
	}
	writeCommand := func(value string) {
		t.Helper()
		if _, err := io.WriteString(stdin, value+"\n"); err != nil {
			t.Fatalf("write child command %q: %v", value, err)
		}
	}
	readLine("ready")
	if err := sensor.TrackProcess(uint32(command.Process.Pid)); err != nil {
		t.Fatalf("track ring overflow target: %v", err)
	}
	baseline, err := sensor.DropCounts()
	if err != nil {
		t.Fatalf("read baseline BPF drop counters: %v", err)
	}
	writeCommand("flood")
	readLine("flood-done")

	quality := &coverageQuality{}
	if err := pollBPFDropCounts(sensor, baseline, quality); err != nil {
		t.Fatalf("poll BPF drop counters after flood: %v", err)
	}
	current, err := sensor.DropCounts()
	if err != nil {
		t.Fatal(err)
	}
	if current[2] <= baseline[2] {
		t.Fatalf("file ring reserve drops did not increase: baseline=%d current=%d", baseline[2], current[2])
	}
	incomplete, detail := quality.report()
	if !incomplete || detail != fmt.Sprintf("file-ring-reserve=%d", current[2]-baseline[2]) {
		t.Fatalf("BPF loss was not classified as sticky incomplete coverage: incomplete=%t detail=%q", incomplete, detail)
	}

	// The ring is deliberately left unread during the flood. Free enough of its
	// bounded capacity before asking the child to emit a path-identifiable marker.
	for i := 0; i < 4096; i++ {
		if _, err := sensor.Read(); err != nil {
			t.Fatalf("drain buffered event %d: %v", i, err)
		}
	}
	writeCommand("marker")
	readCount := 0
	lastEvents := make([]string, 0, 8)
	for {
		event, err := sensor.Read()
		if err != nil {
			t.Fatalf("read recovery marker after %d events (last=%v): %v", readCount, lastEvents, err)
		}
		readCount++
		lastEvents = append(lastEvents, fmt.Sprintf("kind=%s path=%q pid=%d", event.Kind, event.Path, event.Process.PID))
		if len(lastEvents) > 8 {
			lastEvents = lastEvents[1:]
		}
		if event.Kind == core.FileOpen && strings.HasSuffix(event.Path, filepath.Base(markerPath)) {
			break
		}
	}
	readLine("marker-done")
	if err := pollBPFDropCounts(sensor, current, quality); err != nil {
		t.Fatalf("poll BPF counters after marker recovery: %v", err)
	}
	recovered, err := sensor.DropCounts()
	if err != nil {
		t.Fatal(err)
	}
	recoveredIncomplete, recoveredDetail := quality.report()
	if !recoveredIncomplete {
		t.Fatal("observed BPF ring loss did not remain incomplete after recovery")
	}
	writeCommand("continue")
	readLine("continued-done")
	if err := command.Wait(); err != nil {
		t.Fatalf("wait for target continuation: %v", err)
	}

	if err := pollBPFDropCounts(sensor, recovered, quality); err != nil {
		t.Fatalf("poll BPF counters after ring recovery: %v", err)
	}
	after, err := sensor.DropCounts()
	if err != nil {
		t.Fatal(err)
	}
	if after[2] != recovered[2] {
		t.Fatalf("file ring continued dropping after space was recovered: before=%d after=%d", recovered[2], after[2])
	}
	if incomplete, got := quality.report(); !incomplete || got != recoveredDetail {
		t.Fatalf("ring recovery cleared or changed prior loss: incomplete=%t detail=%q want=%q", incomplete, got, recoveredDetail)
	}
}

func ringDropTargetChild(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	protocol := bufio.NewReader(os.Stdin)
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		t.Fatal(err)
	}
	readCommand := func(want string) {
		t.Helper()
		line, err := protocol.ReadString('\n')
		if err != nil || line != want+"\n" {
			t.Fatalf("read command: got %q, err=%v; want %q", line, err, want)
		}
	}
	readCommand("flood")
	for i := 0; i < 200000; i++ {
		file, err := os.Open("/dev/null")
		if err != nil {
			t.Fatalf("flood open %d: %v", i, err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("flood close %d: %v", i, err)
		}
	}
	if _, err := fmt.Fprintln(os.Stdout, "flood-done"); err != nil {
		t.Fatal(err)
	}
	readCommand("marker")
	for i := 0; i < 128; i++ {
		marker, err := os.Open(os.Getenv("AGENTTAINT_RING_DROP_MARKER"))
		if err != nil {
			t.Fatalf("marker open %d: %v", i, err)
		}
		if err := marker.Close(); err != nil {
			t.Fatalf("marker close %d: %v", i, err)
		}
	}
	if _, err := fmt.Fprintln(os.Stdout, "marker-done"); err != nil {
		t.Fatal(err)
	}
	readCommand("continue")
	for i := 0; i < 32; i++ {
		file, err := os.Open("/dev/null")
		if err != nil {
			t.Fatalf("continued open %d: %v", i, err)
		}
		_ = file.Close()
	}
	if _, err := fmt.Fprintln(os.Stdout, "continued-done"); err != nil {
		t.Fatal(err)
	}
}

func TestAuditCoverageLossRemainsStickyAfterQueueRecovers(t *testing.T) {
	quality := &coverageQuality{}
	queue := make(chan core.Event, 1)
	queued := core.FileOpenEvent{Metadata: core.Metadata{EventID: "queued-before-loss"}}
	if err := enqueueAudit(queue, queued, quality); err != nil {
		t.Fatalf("enqueue initial audit event: %v", err)
	}
	if err := enqueueAudit(queue, core.FileOpenEvent{Metadata: core.Metadata{EventID: "dropped"}}, quality); err == nil {
		t.Fatal("full audit queue did not report event loss")
	}

	<-queue
	continued := core.FileOpenEvent{Metadata: core.Metadata{EventID: "continued-after-loss"}}
	if err := enqueueAudit(queue, continued, quality); err != nil {
		t.Fatalf("audit queue did not accept an event after capacity returned: %v", err)
	}
	got, ok := (<-queue).(core.FileOpenEvent)
	if !ok || got.Metadata.EventID != continued.Metadata.EventID {
		t.Fatalf("continued event was not preserved after recovery: %#v", got)
	}

	incomplete, detail := quality.report()
	if !incomplete || detail != "writer-queue-full=1" {
		t.Fatalf("audit loss was not retained after queue recovery: incomplete=%t detail=%q", incomplete, detail)
	}

	process := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 301, BirthID: "loss-recovery", ScopeID: "scope"}, Comm: "agent"}
	processor := newProcessor(t, "deny")
	processEvent(t, processor, fileOpen(sourcePath, process, core.ResultSuccess))
	processor.MarkIncomplete(detail)
	for i := range 2 {
		event := connectEvent(process, core.ResultSuccess)
		event.EventID = "after-loss-" + string(rune('0'+i))
		got := processEvent(t, processor, event)
		if len(got) != 1 || violation(t, got) != nil {
			t.Fatalf("incomplete history produced a policy verdict on continuation %d: %#v", i, got)
		}
	}
	if incomplete, detail = quality.report(); !incomplete || detail != "writer-queue-full=1" {
		t.Fatalf("later processing cleared or changed the original loss: incomplete=%t detail=%q", incomplete, detail)
	}
}
