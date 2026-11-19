package engine

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// The engine reports why a flow stopped through typed errors so callers can
// react with errors.As instead of parsing messages. A single failure can
// carry several of them: a timed-out group yields a *GroupFailedError whose
// chain also holds a *TimeoutError, and an interrupted run wraps whatever the
// interruption caused in a *CanceledError. Callers that rank them should check
// the most specific cause first: canceled, then timed out, then failed.

// GroupFailedError reports that a group's command sequence failed. ExitCode is
// the first failing command's exit status (0 when the process never produced
// one, e.g. the executable was not found; -1 when a signal killed it).
// The message is that of Err, so wrapping adds no noise to existing output.
type GroupFailedError struct {
	Group    string
	ExitCode int
	Err      error
}

func (e *GroupFailedError) Error() string { return e.Err.Error() }

func (e *GroupFailedError) Unwrap() error { return e.Err }

// TimeoutError reports that a group attempt exceeded its envelope timeout. It
// is installed as the context cancellation cause (context.WithTimeoutCause),
// so a deadline can be told apart from a cancel with context.Cause.
type TimeoutError struct {
	Timeout time.Duration
}

func (e *TimeoutError) Error() string { return fmt.Sprintf("timed out after %s", e.Timeout) }

// CanceledError reports that the flow stopped because its context was
// canceled from outside (Ctrl-C, SIGTERM, a caller's cancel) rather than
// because a group failed on its own. Cause is context.Cause of the flow
// context; Err is the failure the cancellation produced.
type CanceledError struct {
	Cause error
	Err   error
}

func (e *CanceledError) Error() string { return fmt.Sprintf("canceled: %v", e.Err) }

func (e *CanceledError) Unwrap() error { return e.Err }

// withTimeout bounds ctx by d, recording a *TimeoutError as the cause so
// timedOut can recognize the deadline later. d <= 0 means no timeout.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeoutCause(ctx, d, &TimeoutError{Timeout: d})
}

// timedOut returns err annotated with the *TimeoutError when ctx ended
// because of its own deadline; otherwise err is returned unchanged. It must
// be called before ctx's cancel func runs, or the cause reads as a cancel.
func timedOut(ctx context.Context, err error) error {
	var te *TimeoutError
	if errors.As(context.Cause(ctx), &te) {
		return fmt.Errorf("%w: %w", te, err)
	}
	return err
}

// canceled wraps err in a *CanceledError when the flow context ctx has been
// canceled; otherwise err is returned unchanged.
func canceled(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	return &CanceledError{Cause: context.Cause(ctx), Err: err}
}
