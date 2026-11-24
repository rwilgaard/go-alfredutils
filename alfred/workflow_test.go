package alfred_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
	"github.com/rwilgaard/go-alfredutils/v2/alfred/alfredtest"
)

func TestItemJSON(t *testing.T) {
	tests := []struct {
		name  string
		build func(r *alfred.Response)
		want  string
	}{
		{
			name:  "items are invalid until Valid(true)",
			build: func(r *alfred.Response) { r.NewItem("a") },
			want:  `{"items":[{"title":"a","valid":false}]}`,
		},
		{
			name:  "an explicitly empty arg is kept",
			build: func(r *alfred.Response) { r.NewItem("a").Arg("") },
			want:  `{"items":[{"title":"a","arg":"","valid":false}]}`,
		},
		{
			name:  "several args are an array",
			build: func(r *alfred.Response) { r.NewItem("a").Arg("x", "y") },
			want:  `{"items":[{"title":"a","arg":["x","y"],"valid":false}]}`,
		},
		{
			name: "modifier keys are sorted and get the item's variables",
			build: func(r *alfred.Response) {
				it := r.NewItem("a").Var("k", "v")
				it.NewModifier(alfred.ModCmd, alfred.ModOpt).Subtitle("").Valid(true)
			},
			want: `{"items":[{"title":"a","valid":false,"variables":{"k":"v"},"mods":{"alt+cmd":{"subtitle":"","valid":true,"variables":{"k":"v"}}}}]}`,
		},
		{
			name: "rerun and cache are clamped to Alfred's limits",
			build: func(r *alfred.Response) {
				r.Rerun(time.Millisecond).Cache(time.Second, true).SkipKnowledge()
			},
			want: `{"items":[],"rerun":0.1,"cache":{"seconds":5,"loosereload":true},"skipknowledge":true}`,
		},
		{
			name:  "Title replaces the title",
			build: func(r *alfred.Response) { r.NewItem("a").Title("b") },
			want:  `{"items":[{"title":"b","valid":false}]}`,
		},
		{
			name: "file items default their arg to the path",
			build: func(r *alfred.Response) {
				r.NewItem("a").File("/tmp/x", true).Copytext("c")
			},
			want: `{"items":[{"title":"a","arg":"/tmp/x","valid":false,"type":"file:skipcheck","text":{"copy":"c"}}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := alfred.NewResponse()
			tt.build(r)
			got, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "empty query keeps the order", query: "", want: []string{"github", "gitlab", "jira", "Confluence"}},
		{name: "drops non-matches", query: "git", want: []string{"github", "gitlab"}},
		{name: "matches Match text instead of the title", query: "wiki", want: []string{"Confluence"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf, out := alfredtest.New(t, nil)
			wf.NewItem("github")
			wf.NewItem("gitlab")
			wf.NewItem("jira")
			wf.NewItem("Confluence").Match("confluence wiki")
			wf.Filter(tt.query)
			wf.Send()
			if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSend(t *testing.T) {
	t.Run("adds the no-results row when empty", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		wf.NoResults("Nothing", "")
		wf.Send()
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"Nothing"}) {
			t.Errorf("got %q", got)
		}
	})

	t.Run("truncates to max results and sends once", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil, alfred.WithMaxResults(2))
		wf.NewItem("a")
		wf.NewItem("b")
		wf.NewItem("c")
		wf.Send()
		wf.Send()
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"a", "b"}) {
			t.Errorf("got %q", got)
		}
	})
}

func TestConfig(t *testing.T) {
	type config struct {
		Name    string        `env:"name"`
		Fuzzy   bool          `env:"fuzzy"`
		Age     int           `env:"age"`
		Every   time.Duration `env:"every"`
		Default string        `env:"default"`
		Ignored string
	}
	tests := []struct {
		name    string
		env     map[string]string
		want    config
		wantErr bool
	}{
		{
			name: "parses each type",
			env:  map[string]string{"name": "x", "fuzzy": "1", "age": "30", "every": "90s"},
			want: config{Name: "x", Fuzzy: true, Age: 30, Every: 90 * time.Second, Default: "keep"},
		},
		{
			name: "a duration in whole seconds",
			env:  map[string]string{"every": "90"},
			want: config{Every: 90 * time.Second, Default: "keep"},
		},
		{
			name: "an empty value keeps the default",
			env:  map[string]string{"default": ""},
			want: config{Default: "keep"},
		},
		{name: "a bad bool is an error", env: map[string]string{"fuzzy": "maybe"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf, _ := alfredtest.New(t, tt.env)
			cfg := config{Default: "keep"}
			err := wf.Config(&cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg != tt.want {
				t.Errorf("got %+v, want %+v", cfg, tt.want)
			}
		})
	}
}

func TestRunMagic(t *testing.T) {
	t.Run("runs the matching action instead of fn", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil, alfred.WithArgs("workflow:reset"))
		var ran bool
		wf.AddMagic("reset", "Reset", "Resetting…", func() error { ran = true; return nil })
		wf.Run(func() { t.Fatal("fn ran") })
		if !ran {
			t.Error("action didn't run")
		}
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"Resetting…"}) {
			t.Errorf("got %q", got)
		}
	})

	t.Run("shows the error when the action fails", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil, alfred.WithArgs("workflow:reset"))
		wf.AddMagic("reset", "Reset", "Resetting…", func() error { return errors.New("disk full") })
		wf.Run(func() { t.Fatal("fn ran") })
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"disk full"}) {
			t.Errorf("got %q", got)
		}
	})

	t.Run("shows a MagicInfo as the result", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil, alfred.WithArgs("workflow:reset"))
		wf.AddMagic("reset", "Reset", "Resetting…", func() error { return alfred.MagicInfo("Nothing to reset") })
		wf.Run(func() { t.Fatal("fn ran") })
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"Nothing to reset"}) {
			t.Errorf("got %q", got)
		}
	})

	t.Run("lists actions for an unknown keyword", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil, alfred.WithArgs("workflow:"))
		wf.AddMagic("reset", "Reset", "Resetting…", func() error { t.Fatal("action ran"); return nil })
		wf.Run(func() { t.Fatal("fn ran") })
		fb := alfredtest.Decode(t, out)
		if len(fb.Items) != 1 || fb.Items[0].Autocomplete != "workflow:reset" {
			t.Errorf("got %+v", fb.Items)
		}
	})

	t.Run("a panic becomes an error row", func(t *testing.T) {
		var code int
		wf, out := alfredtest.New(t, nil, alfred.WithExit(func(c int) { code = c }))
		wf.Run(func() { panic("boom") })
		if code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, []string{"boom"}) {
			t.Errorf("got %q", got)
		}
	})
}

func TestJobs(t *testing.T) {
	wf, _ := alfredtest.New(t, nil)

	if err := wf.Cache.Store("_aw/jobs/stale.pid", []byte("999999")); err != nil {
		t.Fatal(err)
	}
	if wf.IsRunning("stale") {
		t.Error("a pid file for a dead process counts as running")
	}
	if wf.Cache.Exists("_aw/jobs/stale.pid") {
		t.Error("stale pid file wasn't removed")
	}

	if err := wf.RunInBackground("sleep", exec.Command("/bin/sleep", "5")); err != nil {
		t.Fatal(err)
	}
	if !wf.IsRunning("sleep") {
		t.Error("started job isn't running")
	}
	if err := wf.RunInBackground("sleep", exec.Command("/bin/sleep", "5")); !errors.Is(err, alfred.ErrJobExists) {
		t.Errorf("second start: got %v, want ErrJobExists", err)
	}

	if err := wf.Kill("sleep"); err != nil {
		t.Fatal(err)
	}
	if wf.IsRunning("sleep") {
		t.Error("killed job is still running")
	}
}

func TestJobLog(t *testing.T) {
	wf, _ := alfredtest.New(t, nil)
	run := func(script string) string {
		t.Helper()
		if err := wf.RunInBackground("log", exec.Command("/bin/sh", "-c", script)); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for wf.IsRunning("log") {
			if time.Now().After(deadline) {
				t.Fatal("job didn't finish")
			}
			time.Sleep(10 * time.Millisecond)
		}
		b, err := os.ReadFile(wf.JobLog("log"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if got := run("echo out; echo err >&2"); got != "out\nerr\n" {
		t.Errorf("log = %q, want stdout and stderr", got)
	}
	if got := run("echo second"); got != "second\n" {
		t.Errorf("log = %q, want only the latest run", got)
	}
}
