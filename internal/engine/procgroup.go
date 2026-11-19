package engine

import (
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultKillGrace is how long a canceled command's process group gets to
// exit after the graceful stop signal before it is force-killed.
const DefaultKillGrace = 5 * time.Second

// ProcessGroups tracks the process groups of commands that are currently
// running so a caller can force-kill all of them at once, e.g. when the user
// interrupts a second time. A nil *ProcessGroups is valid and tracks nothing.
type ProcessGroups struct {
	mu   sync.Mutex
	pids map[int]struct{}
}

// NewProcessGroups returns an empty tracker.
func NewProcessGroups() *ProcessGroups {
	return &ProcessGroups{pids: map[int]struct{}{}}
}

// KillAll force-kills every tracked process group. It is best-effort: groups
// that already exited are ignored.
func (p *ProcessGroups) KillAll() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for pid := range p.pids {
		_ = killGroup(pid)
	}
}

func (p *ProcessGroups) track(pid int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pids[pid] = struct{}{}
}

func (p *ProcessGroups) untrack(pid int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pids, pid)
}

// runInGroup runs a command built with exec.CommandContext in its own process
// group. When the context is canceled, the whole group is asked to stop
// gracefully; after grace, Wait stops waiting on the command's output and any
// group member still alive is force-killed. grace <= 0 means DefaultKillGrace.
// While the command runs, its group is registered with groups (may be nil).
func runInGroup(cmd *exec.Cmd, grace time.Duration, groups *ProcessGroups) error {
	if grace <= 0 {
		grace = DefaultKillGrace
	}
	var canceledAt atomic.Pointer[time.Time]
	isolateGroup(cmd)
	cmd.Cancel = func() error {
		now := time.Now()
		canceledAt.Store(&now)
		return terminateGroup(cmd.Process)
	}
	cmd.WaitDelay = grace

	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	groups.track(pid)
	defer groups.untrack(pid)

	err := cmd.Wait()
	if t := canceledAt.Load(); t != nil {
		reapGroup(pid, t.Add(grace))
	}
	return err
}
