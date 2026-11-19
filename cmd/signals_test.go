package cmd

import (
	"bytes"
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const handlerWait = 2 * time.Second

// handlerHarness drives an interruptHandler with fake signals and records
// what it did instead of touching real processes or exiting.
type handlerHarness struct {
	signals   chan os.Signal
	ctx       context.Context
	killed    atomic.Bool
	exitCodes chan int
	stderr    bytes.Buffer
	done      chan struct{}
	finished  chan struct{}
}

func startHandler(t *testing.T) *handlerHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &handlerHarness{
		signals:   make(chan os.Signal),
		ctx:       ctx,
		exitCodes: make(chan int, 1),
		done:      make(chan struct{}),
		finished:  make(chan struct{}),
	}
	handler := interruptHandler{
		signals:   h.signals,
		cancel:    cancel,
		forceKill: func() { h.killed.Store(true) },
		exit:      func(code int) { h.exitCodes <- code },
		stderr:    &h.stderr,
	}
	go func() {
		defer close(h.finished)
		handler.run(h.done)
	}()
	return h
}

func (h *handlerHarness) send(t *testing.T) {
	t.Helper()
	select {
	case h.signals <- os.Interrupt:
	case <-time.After(handlerWait):
		t.Fatal("handler is not receiving signals")
	}
}

func TestInterruptHandler_FirstSignalCancelsGracefully(t *testing.T) {
	t.Parallel()
	h := startHandler(t)

	h.send(t)

	select {
	case <-h.ctx.Done():
	case <-time.After(handlerWait):
		t.Fatal("first signal did not cancel the context")
	}
	assert.False(t, h.killed.Load(), "first signal must not force-kill")
	assert.Empty(t, h.exitCodes, "first signal must not exit")
	close(h.done)
	<-h.finished
	assert.Contains(t, h.stderr.String(), "again to force")
}

func TestInterruptHandler_SecondSignalForceKillsAndExits130(t *testing.T) {
	t.Parallel()
	h := startHandler(t)

	h.send(t)
	h.send(t)

	select {
	case code := <-h.exitCodes:
		assert.Equal(t, 130, code)
	case <-time.After(handlerWait):
		t.Fatal("second signal did not exit")
	}
	assert.True(t, h.killed.Load(), "second signal must force-kill process groups")
	<-h.finished
}

func TestInterruptHandler_StopsWhenDoneWithoutSignals(t *testing.T) {
	t.Parallel()
	h := startHandler(t)

	close(h.done)

	select {
	case <-h.finished:
	case <-time.After(handlerWait):
		t.Fatal("handler did not stop when done closed")
	}
	require.NoError(t, h.ctx.Err(), "stopping must not cancel the run")
	assert.False(t, h.killed.Load())
}
