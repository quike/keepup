package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
)

// exitCodeInterrupted is the conventional status of a process ended by
// SIGINT (128 + 2), used when a repeated signal forces keepup to exit.
const exitCodeInterrupted = 130

// interruptHandler escalates termination signals. The first one cancels the
// run so commands get their graceful stop; the next one force-kills whatever
// is still running and exits immediately. Its collaborators are injected so
// the escalation can be tested without real signals, processes, or exits.
type interruptHandler struct {
	signals   <-chan os.Signal
	cancel    context.CancelFunc
	forceKill func()
	exit      func(code int)
	stderr    io.Writer
}

// run handles signals until done is closed or a repeated signal forces exit.
func (h interruptHandler) run(done <-chan struct{}) {
	select {
	case <-done:
		return
	case sig := <-h.signals:
		fmt.Fprintf(h.stderr, "received %s, stopping; send it again to force quit\n", sig)
		h.cancel()
	}
	select {
	case <-done:
	case <-h.signals:
		h.forceKill()
		h.exit(exitCodeInterrupted)
	}
}
