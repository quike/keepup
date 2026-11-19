//go:build !windows

package engine

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// reapPollInterval is how often reapGroup checks whether a canceled group
// has exited during its grace period.
const reapPollInterval = 20 * time.Millisecond

// isolateGroup starts the command as the leader of a new process group, so
// signals sent to the group reach every process it forks.
func isolateGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateGroup asks the whole process group led by p to stop (SIGTERM).
func terminateGroup(p *os.Process) error {
	return signalGroup(p.Pid, syscall.SIGTERM)
}

// killGroup force-kills the whole process group led by pid (SIGKILL).
func killGroup(pid int) error {
	return signalGroup(pid, syscall.SIGKILL)
}

// reapGroup waits until deadline for the group's remaining members (the
// leader is already reaped) to exit, then force-kills whatever is left.
func reapGroup(pid int, deadline time.Time) {
	for time.Now().Before(deadline) {
		if signalGroup(pid, 0) != nil {
			return
		}
		time.Sleep(reapPollInterval)
	}
	_ = killGroup(pid)
}

// signalGroup sends sig to the process group pid, reporting a group with no
// members left as os.ErrProcessDone (which exec.Cmd treats as benign).
func signalGroup(pid int, sig syscall.Signal) error {
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
