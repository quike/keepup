package cmd

import (
	"errors"

	"github.com/quike/keepup/internal/config"
	"github.com/quike/keepup/internal/engine"
)

// Process exit codes. 124 and 130 follow the timeout(1) and shell (128+SIGINT)
// conventions so wrappers can treat keepup like any other Unix tool.
const (
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitTimeout  = 124
	exitCanceled = 130
)

// usageError marks an invocation cobra rejected before any command ran: an
// unknown command or flag, or the wrong number of arguments. It is the CLI's
// own counterpart to *config.Error.
type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

// exitCode maps an invocation's error to the process exit code. It is the
// only place the mapping lives. The checks run from the most specific cause
// to the least, because one failure can carry several typed errors (a
// cancellation usually surfaces as a failed group). With passthrough
// (--exit-code) a failed group exits with its command's own status when that
// status is a real, positive exit code.
func exitCode(err error, passthrough bool) int {
	if err == nil {
		return exitOK
	}
	var (
		canceled  *engine.CanceledError
		timeout   *engine.TimeoutError
		cfgErr    *config.Error
		usage     *usageError
		failedGrp *engine.GroupFailedError
	)
	switch {
	case errors.As(err, &canceled):
		return exitCanceled
	case errors.As(err, &timeout):
		return exitTimeout
	case errors.As(err, &cfgErr), errors.As(err, &usage):
		return exitUsage
	case passthrough && errors.As(err, &failedGrp) && failedGrp.ExitCode > 0:
		return failedGrp.ExitCode
	default:
		return exitFailure
	}
}
