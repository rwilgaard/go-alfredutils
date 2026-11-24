package clipboard

import (
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred/alfredtest"
)

// fakeExec answers the copy script with a change count and turns the clear
// job into a sleep, recording the clear job's arguments. The real pasteboard
// is never touched.
func fakeExec(t *testing.T, clearArgs *[][]string) {
	t.Helper()
	orig := execCommand
	t.Cleanup(func() { execCommand = orig })
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "/bin/sh" {
			*clearArgs = append(*clearArgs, args)
			return exec.Command("/bin/sleep", "5")
		}
		return exec.Command("/bin/echo", "42")
	}
}

func TestCopy(t *testing.T) {
	var clears [][]string
	fakeExec(t, &clears)
	n, err := Copy("secret")
	if err != nil || n != 42 {
		t.Fatalf("Copy() = %d, %v, want 42", n, err)
	}
}

func TestCopySecret(t *testing.T) {
	t.Run("schedules a clear for the new change count", func(t *testing.T) {
		var clears [][]string
		fakeExec(t, &clears)
		wf, _ := alfredtest.New(t, nil)
		t.Cleanup(func() { _ = wf.Kill(ClearJob) })

		if err := CopySecret(wf, "secret", 1500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if !wf.IsRunning(ClearJob) {
			t.Fatal("clear job not running")
		}
		if len(clears) != 1 {
			t.Fatalf("clear scheduled %d times", len(clears))
		}
		args := clears[0]
		if args[0] != "-c" || args[1] != clearShell || !slices.Equal(args[2:3], []string{"sh"}) ||
			args[3] != "1.5" || args[4] != clearScript || args[5] != "42" {
			t.Errorf("clear args = %q", args)
		}
	})

	t.Run("a newer copy replaces the pending clear", func(t *testing.T) {
		var clears [][]string
		fakeExec(t, &clears)
		wf, _ := alfredtest.New(t, nil)
		t.Cleanup(func() { _ = wf.Kill(ClearJob) })

		for range 2 {
			if err := CopySecret(wf, "secret", time.Minute); err != nil {
				t.Fatal(err)
			}
		}
		if len(clears) != 2 || !wf.IsRunning(ClearJob) {
			t.Errorf("clears=%d running=%v", len(clears), wf.IsRunning(ClearJob))
		}
	})

	t.Run("no clear without a timeout", func(t *testing.T) {
		var clears [][]string
		fakeExec(t, &clears)
		wf, _ := alfredtest.New(t, nil)
		if err := CopySecret(wf, "secret", 0); err != nil {
			t.Fatal(err)
		}
		if len(clears) != 0 || wf.IsRunning(ClearJob) {
			t.Error("clear scheduled with no timeout")
		}
	})
}
