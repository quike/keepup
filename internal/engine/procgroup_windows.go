//go:build windows

package engine

import (
	"os"
	"os/exec"
	"time"
)

// Windows has no POSIX process groups or SIGTERM: cancellation keeps the
// exec.CommandContext behavior of killing the direct child. A Job Object
// could extend this to the whole tree later.

func isolateGroup(*exec.Cmd) {}

func terminateGroup(p *os.Process) error {
	return p.Kill()
}

func killGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func reapGroup(int, time.Time) {}
