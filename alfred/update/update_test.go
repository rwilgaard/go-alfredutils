package update

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rwilgaard/go-alfredutils/v2/alfred/alfredtest"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"v1.2.0", "1.1.9", true},
		{"1.10.0", "1.9.0", true},
		{"v1.2.0", "v1.2.0", false},
		{"1.2.0", "1.2.0-beta", true},
		{"1.2.0-beta", "1.2.0", false},
		{"1.2.0-rc.1", "1.2.0-beta", true},
		{"2.0", "1.9.9", true},
		{"1.0.0", "1.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			if got := newer(tt.a, tt.b); got != tt.want {
				t.Errorf("newer(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	const releases = `[
		{"tag_name": "v1.3.0", "prerelease": true, "assets": [{"name": "w.alfredworkflow", "browser_download_url": "https://x/pre"}]},
		{"tag_name": "v1.2.0", "assets": [{"name": "w.alfredworkflow", "browser_download_url": "https://x/1.2.0"}]},
		{"tag_name": "v1.2.1", "assets": [{"name": "notes.txt", "browser_download_url": "https://x/notes"}]},
		{"tag_name": "v1.1.0", "assets": [{"name": "w.alfredworkflow", "browser_download_url": "https://x/1.1.0"}]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			t.Errorf("requested %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(releases)) //nolint:errcheck // test server
	}))
	defer srv.Close()

	tests := []struct {
		version string
		want    bool
	}{
		{"1.1.0", true},
		{"1.2.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			wf, _ := alfredtest.New(t, map[string]string{"alfred_workflow_version": tt.version})
			u := GitHub(wf, "o/r")
			u.apiURL = srv.URL
			if err := u.Check(t.Context()); err != nil {
				t.Fatal(err)
			}

			r, ok := u.latest()
			if ok != tt.want {
				t.Errorf("update available = %v, want %v", ok, tt.want)
			}
			// Skips the pre-release and the release without a workflow.
			if r.URL != "https://x/1.2.0" {
				t.Errorf("latest URL = %q", r.URL)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		value string
		set   bool
		want  bool
	}{
		{set: false, want: true},
		{value: "", set: true, want: true},
		{value: "1", set: true, want: true},
		{value: "true", set: true, want: true},
		{value: "0", set: true, want: false},
		{value: "false", set: true, want: false},
		{value: "junk", set: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			env := map[string]string{"alfred_workflow_version": "1.0.0"}
			if tt.set {
				env[enabledVar] = tt.value
			}
			wf, _ := alfredtest.New(t, env)
			if got := GitHub(wf, "o/r").Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
