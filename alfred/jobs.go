package alfred

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrJobExists is returned when a job of the same name is already running.
var ErrJobExists = errors.New("job already running")

// jobsDir is where awgo keeps its pid files, so a job started by an awgo
// build is still seen right after the workflow updates.
const jobsDir = "_aw/jobs"

// RunInBackground starts cmd in its own process group, so it outlives the
// Script Filter that started it.
func (wf *Workflow) RunInBackground(name string, cmd *exec.Cmd) error {
	if pid, ok := wf.jobPid(name); ok {
		return fmt.Errorf("%w: %s (pid %d)", ErrJobExists, name, pid)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start job %s: %w", name, err)
	}
	// Reap the child when it exits; nothing waits on it otherwise.
	go cmd.Wait() //nolint:errcheck // the job's exit status isn't used
	return wf.Cache.Store(pidKey(name), []byte(strconv.Itoa(cmd.Process.Pid)))
}

// Start runs this binary with args as job name. It returns false when the
// job is already running.
func (wf *Workflow) Start(name string, args ...string) (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	err = wf.RunInBackground(name, exec.Command(exe, args...))
	if errors.Is(err, ErrJobExists) {
		return false, nil
	}
	return err == nil, err
}

func (wf *Workflow) IsRunning(name string) bool {
	_, ok := wf.jobPid(name)
	return ok
}

// Kill sends SIGTERM to a running job.
func (wf *Workflow) Kill(name string) error {
	pid, ok := wf.jobPid(name)
	if !ok {
		return nil
	}
	err := syscall.Kill(pid, syscall.SIGTERM)
	if derr := wf.Cache.Delete(pidKey(name)); err == nil {
		err = derr
	}
	return err
}

// jobPid returns the pid of a running job and removes a stale pid file.
func (wf *Workflow) jobPid(name string) (int, bool) {
	data, err := wf.Cache.Load(pidKey(name))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err == nil && syscall.Kill(pid, 0) == nil {
		return pid, true
	}
	_ = wf.Cache.Delete(pidKey(name)) //nolint:errcheck // stale file; IsRunning is already answered
	return 0, false
}

func pidKey(name string) string { return filepath.Join(jobsDir, name+".pid") }
