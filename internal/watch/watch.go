package watch

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/quike/keepup/internal/logger"
)

// DefaultDebounce coalesces bursts of filesystem events (editors often emit
// several per save) into a single re-run.
const DefaultDebounce = 200 * time.Millisecond

// Event is a filesystem change notification carrying the affected path.
type Event struct{ Path string }

// Source is an injectable stream of filesystem events. Production code uses
// the fsnotify-backed source; tests use a fake.
type Source interface {
	Events() <-chan Event
	Errors() <-chan error
	Add(path string) error
	Close() error
}

// Watcher re-runs a callback when files matching its patterns change.
type Watcher struct {
	patterns   []string
	src        Source
	debounce   time.Duration
	log        logger.Logger
	initialRun bool
}

// Option configures a Watcher.
type Option func(*Watcher)

// WithDebounce overrides the default debounce window.
func WithDebounce(d time.Duration) Option { return func(w *Watcher) { w.debounce = d } }

// WithLogger sets the logger (default: no-op).
func WithLogger(l logger.Logger) Option { return func(w *Watcher) { w.log = l } }

// WithInitialRun controls whether onChange fires once before watching
// (default true).
func WithInitialRun(b bool) Option { return func(w *Watcher) { w.initialRun = b } }

// New builds a Watcher over the given patterns and event source.
func New(patterns []string, src Source, opts ...Option) *Watcher {
	w := &Watcher{
		patterns:   patterns,
		src:        src,
		debounce:   DefaultDebounce,
		log:        logger.Nop(),
		initialRun: true,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Run blocks, invoking onChange on each debounced batch of matching changes,
// until ctx is canceled. The slice passed to onChange contains the
// deduplicated, sorted list of matched paths accumulated during the just-closed
// debounce window. The initial run, when enabled, passes a nil slice — it was
// not triggered by any file. A failing onChange is logged but does not stop
// the watch — the whole point is to keep iterating. New directories that
// appear under watched trees are added automatically.
func (w *Watcher) Run(ctx context.Context, onChange func(context.Context, []string) error) error {
	if w.initialRun {
		w.invoke(ctx, onChange, nil)
	}

	var debounceC <-chan time.Time
	pending := make(map[string]struct{})
	for {
		select {
		case <-ctx.Done():
			return nil

		case ev := <-w.src.Events():
			// Auto-watch newly created directories so deeper files are seen.
			if isDir(ev.Path) {
				_ = w.src.Add(ev.Path)
			}
			if Matches(w.patterns, ev.Path) {
				w.log.Debug("change detected", "path", ev.Path)
				pending[ev.Path] = struct{}{}
				debounceC = time.After(w.debounce)
			}

		case <-debounceC:
			debounceC = nil
			files := make([]string, 0, len(pending))
			for p := range pending {
				files = append(files, p)
				delete(pending, p)
			}
			sort.Strings(files)
			w.invoke(ctx, onChange, files)

		case err := <-w.src.Errors():
			if err != nil {
				w.log.Warn("watch source error", "err", err.Error())
			}
		}
	}
}

func (w *Watcher) invoke(ctx context.Context, onChange func(context.Context, []string) error, files []string) {
	if err := onChange(ctx, files); err != nil {
		w.log.Error("run failed; continuing to watch", "err", err.Error())
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
