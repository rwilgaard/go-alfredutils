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
	"time"
)

// ErrJobExists is returned when a job of the same name is already running.
var ErrJobExists = errors.New("job already running")

// jobsDir is where awgo keeps its pid files, so a job started by an awgo
// build is still seen right after the workflow updates.
const jobsDir = "_aw/jobs"

// RunInBackground starts cmd in its own process group, so it outlives the
// Script Filter that started it. Unless cmd already has an output, its stdout
// and stderr go to JobLog(name), replacing the previous run's log.
func (wf *Workflow) RunInBackground(name string, cmd *exec.Cmd) error {
	if pid, ok := wf.jobPid(name); ok {
		return fmt.Errorf("%w: %s (pid %d)", ErrJobExists, name, pid)
	}
	if cmd.Stdout == nil && cmd.Stderr == nil {
		logFile, err := wf.createJobLog(name)
		if err != nil {
			return err
		}
		// The child has its own copy of the descriptor once started.
		defer logFile.Close() //nolint:errcheck // nothing was written through this handle
		cmd.Stdout, cmd.Stderr = logFile, logFile
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

// JobLog returns the path of the log a background job writes to.
func (wf *Workflow) JobLog(name string) string {
	return wf.Cache.Path(filepath.Join(jobsDir, name+".log"))
}

func (wf *Workflow) createJobLog(name string) (*os.File, error) {
	path := wf.JobLog(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.Create(path) //nolint:gosec // path is built from the cache folder and a job name the workflow chose
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

// RefreshIfStale keeps a cached key fresh from a Script Filter. When key is
// missing or older than maxAge, it starts job (this binary with args, or with
// job as its only argument when args is empty) and reruns the Script Filter
// every 2 s to pick up the result, and it reports true. While key is missing
// altogether, the no-results row says "Refreshing cache…", so a NoResults
// call after this replaces that.
//
// Use the same job name everywhere the job is started, so two refreshes never
// run at once.
func (wf *Workflow) RefreshIfStale(key string, maxAge time.Duration, job string, args ...string) (bool, error) {
	if !wf.Cache.Expired(key, maxAge) {
		return false, nil
	}
	if len(args) == 0 {
		args = []string{job}
	}
	if _, err := wf.Start(job, args...); err != nil {
		return false, err
	}
	wf.Rerun(2 * time.Second)
	if !wf.Cache.Exists(key) {
		wf.NoResults("Refreshing cache…", "Results appear in a moment")
	}
	return true, nil
}
