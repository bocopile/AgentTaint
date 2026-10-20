package taint

import (
	"errors"
	"fmt"
	"sort"

	"github.com/bocopile/AgentTaint/internal/core"
)

// Engine applies the Phase 3 single-label reference model to an ordered event
// stream. Source paths are compared verbatim; they are not resolved or cleaned.
type Engine struct {
	sources          map[string]struct{}
	labels           map[core.ProcessKey]map[core.Label]struct{}
	sourcesByProcess map[core.ProcessKey]map[string]struct{}
	complete         bool
	problems         []string
}

func NewEngine(sourcePaths []string) (*Engine, error) {
	sources := make(map[string]struct{}, len(sourcePaths))
	for _, path := range sourcePaths {
		if path == "" {
			return nil, errors.New("source path must not be empty")
		}
		sources[path] = struct{}{}
	}
	return &Engine{sources: sources, labels: make(map[core.ProcessKey]map[core.Label]struct{}), sourcesByProcess: make(map[core.ProcessKey]map[string]struct{}), complete: true}, nil
}

// Apply consumes one event in the order supplied by the observer. It does not
// sort by Timestamp, which is wall-clock metadata and does not guarantee order.
func (e *Engine) Apply(event core.Event) error {
	if e == nil {
		return errors.New("nil taint engine")
	}
	if event == nil {
		return errors.New("nil event")
	}
	switch v := event.(type) {
	case core.FileOpenEvent:
		return e.applyFileOpen(v)
	case *core.FileOpenEvent:
		if v == nil {
			return errors.New("nil file-open event")
		}
		return e.applyFileOpen(*v)
	case core.ProcessForkEvent:
		return e.applyFork(v)
	case *core.ProcessForkEvent:
		if v == nil {
			return errors.New("nil process-fork event")
		}
		return e.applyFork(*v)
	case core.ProcessExitEvent:
		return e.applyExit(v)
	case *core.ProcessExitEvent:
		if v == nil {
			return errors.New("nil process-exit event")
		}
		return e.applyExit(*v)
	case core.ProcessExecEvent, *core.ProcessExecEvent,
		core.FileReadEvent, *core.FileReadEvent,
		core.FileWriteEvent, *core.FileWriteEvent,
		core.NetworkConnectEvent, *core.NetworkConnectEvent:
		return nil
	default:
		return fmt.Errorf("unsupported event type %T", event)
	}
}

func (e *Engine) applyFileOpen(event core.FileOpenEvent) error {
	if !validKey(event.Process.ProcessKey) {
		return errors.New("file-open event has invalid process key")
	}
	if event.ObservationStage != core.StageCompleted || event.OperationResult != core.ResultSuccess {
		return nil
	}
	if _, source := e.sources[event.Resource.Path]; !source {
		return nil
	}
	e.add(event.Process.ProcessKey, core.LabelSecret)
	e.addSource(event.Process.ProcessKey, event.Resource.Path)
	return nil
}

func (e *Engine) applyFork(event core.ProcessForkEvent) error {
	parent := event.Process.ProcessKey
	child := event.Child.ProcessKey
	if !validKey(parent) || !validKey(child) {
		return errors.New("process-fork event has invalid process key")
	}
	if parent.ScopeID != child.ScopeID {
		return errors.New("process-fork event crosses observation scopes")
	}
	if parent.PID == child.PID {
		return errors.New("process-fork event cannot represent a thread sharing its parent PID")
	}
	for _, label := range e.Labels(parent) {
		e.add(child, label)
	}
	for _, path := range e.SourcePaths(parent) {
		e.addSource(child, path)
	}
	return nil
}

func (e *Engine) applyExit(event core.ProcessExitEvent) error {
	key := event.Process.ProcessKey
	if !validKey(key) {
		return errors.New("process-exit event has invalid process key")
	}
	delete(e.labels, key)
	delete(e.sourcesByProcess, key)
	return nil
}

func validKey(key core.ProcessKey) bool {
	return key.PID != 0 && key.BirthID != "" && key.ScopeID != ""
}

func (e *Engine) add(key core.ProcessKey, label core.Label) {
	set := e.labels[key]
	if set == nil {
		set = make(map[core.Label]struct{})
		e.labels[key] = set
	}
	set[label] = struct{}{}
}

func (e *Engine) addSource(key core.ProcessKey, path string) {
	set := e.sourcesByProcess[key]
	if set == nil {
		set = make(map[string]struct{})
		e.sourcesByProcess[key] = set
	}
	set[path] = struct{}{}
}

// SourcePaths returns the sorted source paths that contributed to this process label.
func (e *Engine) SourcePaths(key core.ProcessKey) []string {
	if e == nil {
		return nil
	}
	paths := make([]string, 0, len(e.sourcesByProcess[key]))
	for path := range e.sourcesByProcess[key] {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// Labels returns a stable, sorted copy of the process labels.
func (e *Engine) Labels(key core.ProcessKey) []core.Label {
	if e == nil {
		return nil
	}
	set := e.labels[key]
	labels := make([]core.Label, 0, len(set))
	for label := range set {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i] < labels[j] })
	return labels
}

func (e *Engine) HasLabel(key core.ProcessKey, label core.Label) bool {
	if e == nil {
		return false
	}
	_, ok := e.labels[key][label]
	return ok
}

// MarkIncomplete records an out-of-band observer loss. The engine cannot infer
// missing events from the event stream itself, so callers must pass run health.
func (e *Engine) MarkIncomplete(reason string) {
	if e == nil {
		return
	}
	e.complete = false
	if reason != "" {
		e.problems = append(e.problems, reason)
	}
}

func (e *Engine) Complete() bool { return e != nil && e.complete }

func (e *Engine) Problems() []string {
	if e == nil {
		return nil
	}
	return append([]string(nil), e.problems...)
}
