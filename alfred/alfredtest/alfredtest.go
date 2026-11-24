// Package alfredtest runs workflow code in tests, without Alfred.
package alfredtest

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
)

// New returns a Workflow with temp cache and data dirs, a throwaway bundle
// ID, and its output captured in the returned buffer. env adds or overrides
// variables. FatalError calls t.Fatal instead of exiting.
func New(t testing.TB, env map[string]string, opts ...alfred.Option) (*alfred.Workflow, *bytes.Buffer) {
	t.Helper()
	vars := map[string]string{
		"alfred_workflow_bundleid": "test.alfredtest",
		"alfred_workflow_name":     "Test",
		"alfred_workflow_cache":    t.TempDir(),
		"alfred_workflow_data":     t.TempDir(),
	}
	for k, v := range env {
		vars[k] = v
	}
	var out bytes.Buffer
	opts = append([]alfred.Option{
		alfred.WithEnv(vars),
		alfred.WithStdout(&out),
		alfred.WithArgs(),
		alfred.WithExit(func(code int) { t.Fatalf("workflow exited with %d: %s", code, out.String()) }),
	}, opts...)
	wf, err := alfred.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return wf, &out
}

// Feedback is decoded Script Filter output.
type Feedback struct {
	Items     []Item            `json:"items"`
	Rerun     float64           `json:"rerun"`
	Variables map[string]string `json:"variables"`
}

type Item struct {
	Title        string              `json:"title"`
	Subtitle     string              `json:"subtitle"`
	Arg          any                 `json:"arg"`
	Valid        bool                `json:"valid"`
	Autocomplete string              `json:"autocomplete"`
	Variables    map[string]string   `json:"variables"`
	Mods         map[string]Modifier `json:"mods"`
}

type Modifier struct {
	Subtitle  string            `json:"subtitle"`
	Arg       any               `json:"arg"`
	Valid     bool              `json:"valid"`
	Variables map[string]string `json:"variables"`
}

// Decode parses the output captured by New.
func Decode(t testing.TB, out *bytes.Buffer) Feedback {
	t.Helper()
	var fb Feedback
	if err := json.Unmarshal(out.Bytes(), &fb); err != nil {
		t.Fatalf("decode feedback %q: %v", out.String(), err)
	}
	return fb
}

// Titles returns the item titles in order.
func (fb Feedback) Titles() []string {
	titles := make([]string, len(fb.Items))
	for i, it := range fb.Items {
		titles[i] = it.Title
	}
	return titles
}
