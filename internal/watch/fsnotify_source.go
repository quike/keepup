package watch

import (
	"github.com/fsnotify/fsnotify"
)

// fsnotifySource adapts fsnotify to the Source interface.
type fsnotifySource struct {
	w      *fsnotify.Watcher
	events chan Event
	errs   chan error
}

// NewFSNotifySource returns a Source backed by fsnotify. Call Add for each
// directory to watch; Close releases the underlying watcher.
func NewFSNotifySource() (Source, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	s := &fsnotifySource{
		w:      w,
		events: make(chan Event),
		errs:   make(chan error, 1),
	}
	go s.pump()
	return s, nil
}

func (s *fsnotifySource) pump() {
	for {
		select {
		case ev, ok := <-s.w.Events:
			if !ok {
				close(s.events)
				return
			}
			s.events <- Event{Path: ev.Name}
		case err, ok := <-s.w.Errors:
			if !ok {
				return
			}
			select {
			case s.errs <- err:
			default:
			}
		}
	}
}

func (s *fsnotifySource) Events() <-chan Event  { return s.events }
func (s *fsnotifySource) Errors() <-chan error  { return s.errs }
func (s *fsnotifySource) Add(path string) error { return s.w.Add(path) }
func (s *fsnotifySource) Close() error          { return s.w.Close() }
