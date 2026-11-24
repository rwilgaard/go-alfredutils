package alfred_test

import (
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
	"github.com/rwilgaard/go-alfredutils/v2/alfred/alfredtest"
)

func TestRefreshIfStale(t *testing.T) {
	// Each case holds the job name with a sleep, so RefreshIfStale never
	// starts the test binary itself.
	busy := func(t *testing.T, wf *alfred.Workflow) {
		t.Helper()
		if err := wf.RunInBackground("cache", exec.Command("/bin/sleep", "5")); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = wf.Kill("cache") })
	}

	t.Run("fresh key does nothing", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		if err := wf.Cache.StoreJSON("items.json", []string{"a"}); err != nil {
			t.Fatal(err)
		}
		refreshing, err := wf.RefreshIfStale("items.json", time.Hour, "cache")
		if err != nil || refreshing {
			t.Fatalf("got %v, %v", refreshing, err)
		}
		wf.Send()
		if fb := alfredtest.Decode(t, out); fb.Rerun != 0 {
			t.Errorf("rerun = %v", fb.Rerun)
		}
	})

	t.Run("stale key reruns and keeps the old results", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		busy(t, wf)
		if err := wf.Cache.StoreJSON("items.json", []string{"a"}); err != nil {
			t.Fatal(err)
		}
		refreshing, err := wf.RefreshIfStale("items.json", 0, "cache")
		if err != nil || !refreshing {
			t.Fatalf("got %v, %v", refreshing, err)
		}
		wf.Send()
		fb := alfredtest.Decode(t, out)
		if fb.Rerun != 2 || !slices.Equal(fb.Titles(), []string{"No results found..."}) {
			t.Errorf("rerun = %v, titles = %q", fb.Rerun, fb.Titles())
		}
	})

	t.Run("missing key shows refreshing", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		busy(t, wf)
		if _, err := wf.RefreshIfStale("items.json", time.Hour, "cache"); err != nil {
			t.Fatal(err)
		}
		wf.Filter("query")
		wf.Send()
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"Refreshing cache…"}) {
			t.Errorf("titles = %q", got)
		}
	})
}
