package engine

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Event names emitted over the run lifecycle.
const (
	EventFlowStart  = "flow.start"
	EventFlowEnd    = "flow.end"
	EventGroupStart = "group.start"
	EventGroupEnd   = "group.end"
)

// Group end statuses.
const (
	StatusOK       = "ok"
	StatusFailed   = "failed"
	StatusSkipped  = "skipped"
	StatusCacheHit = "cache-hit"
	StatusDryRun   = "dry-run"
)

// Event is a single structured run event for machine consumption (CI tooling).
type Event struct {
	Event      string    `json:"event"`
	Flow       string    `json:"flow,omitempty"`
	Group      string    `json:"group,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Status     string    `json:"status,omitempty"`
	DurationMS int64     `json:"durationMs,omitempty"`
	Err        string    `json:"err,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Time       time.Time `json:"time"`
}

// Emitter receives lifecycle events. Implementations must be safe for
// concurrent use (groups run in parallel).
type Emitter interface {
	Emit(Event)
}

// nopEmitter discards events; the engine default.
type nopEmitter struct{}

func (nopEmitter) Emit(Event) {}

// JSONEmitter writes one JSON object per line to a writer.
type JSONEmitter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONEmitter returns an Emitter that writes newline-delimited JSON to w.
func NewJSONEmitter(w io.Writer) *JSONEmitter { return &JSONEmitter{w: w} }

// Emit serializes ev as a single JSON line. Encoding errors are dropped — the
// event stream is best-effort observability, never load-bearing.
func (e *JSONEmitter) Emit(ev Event) { //nolint:gocritic // value receiver keeps the Emitter interface simple
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	b, err := json.Marshal(&ev)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.w.Write(append(b, '\n'))
}

func msSince(start time.Time) int64 { return time.Since(start).Milliseconds() }

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
