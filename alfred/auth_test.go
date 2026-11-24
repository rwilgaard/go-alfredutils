package alfred

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSecurity stands in for /usr/bin/security with one stored password, so
// tests never touch the login Keychain. It logs its argv to argv.log.
const fakeSecurity = `#!/bin/sh
dir=$(dirname "$0")
echo "$@" >> "$dir/argv.log"
case "$1" in
  -i) cat > "$dir/stdin"; touch "$dir/stored" ;;
  find-generic-password) [ -f "$dir/stored" ] || exit 44; echo 'password: "s3cret"' >&2 ;;
  delete-generic-password) [ -f "$dir/stored" ] || exit 44; rm "$dir/stored" ;;
esac
`

func newAuthTest(t *testing.T) (*Workflow, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "security")
	if err := os.WriteFile(bin, []byte(fakeSecurity), 0o700); err != nil { //nolint:gosec // test script must be executable
		t.Fatal(err)
	}
	old := securityBin
	securityBin = bin
	t.Cleanup(func() { securityBin = old })

	wf, err := New(
		WithEnv(map[string]string{
			"alfred_workflow_bundleid": "test.auth",
			"alfred_workflow_cache":    t.TempDir(),
			"alfred_workflow_data":     t.TempDir(),
		}),
		WithStdout(&bytes.Buffer{}),
		WithExit(func(int) { t.Fatal("exited") }),
	)
	if err != nil {
		t.Fatal(err)
	}
	return wf, dir
}

func TestAuthMarker(t *testing.T) {
	wf, dir := newAuthTest(t)
	const account = "acct"
	stored := filepath.Join(dir, "stored")

	if err := wf.SetAuth(account, `pa"ss word`); err != nil {
		t.Fatal(err)
	}
	argv, err := os.ReadFile(filepath.Join(dir, "argv.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argv), "ss word") {
		t.Errorf("secret on the command line: %q", argv)
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `add-generic-password -U -s "test.auth" -a "acct" -w "pa\"ss word"` + "\n"; string(stdin) != want {
		t.Errorf("stdin = %q, want %q", stdin, want)
	}

	// With the marker in place, RequireAuth must not need the Keychain.
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	if !wf.RequireAuth(account) {
		t.Error("RequireAuth = false with a marker")
	}

	// LoadAuth finds the entry gone and drops the stale marker.
	if _, err := wf.LoadAuth(account); err != ErrNotFound {
		t.Fatalf("LoadAuth: got %v, want ErrNotFound", err)
	}
	if wf.RequireAuth(account) {
		t.Error("RequireAuth = true after the stale marker was dropped")
	}

	// An entry stored without SetAuth, as before markers, still counts and
	// gets a marker.
	if err := os.WriteFile(stored, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wf2, _ := newAuthTestWith(t, wf)
	if !wf2.RequireAuth(account) {
		t.Error("RequireAuth = false for a Keychain entry without a marker")
	}
	if !wf2.Data.Exists(authMarkerKey(account)) {
		t.Error("marker not written")
	}

	if err := wf2.ClearAuth(account); err != nil {
		t.Fatal(err)
	}
	if err := wf2.ClearAuth(account); err != nil {
		t.Errorf("second ClearAuth: %v", err)
	}
	if wf2.Data.Exists(authMarkerKey(account)) {
		t.Error("marker left after ClearAuth")
	}
}

// newAuthTestWith returns a fresh Workflow (unsent output) sharing wf's folders.
func newAuthTestWith(t *testing.T, wf *Workflow) (*Workflow, error) {
	t.Helper()
	return New(
		WithEnv(map[string]string{
			"alfred_workflow_bundleid": wf.Env.BundleID,
			"alfred_workflow_cache":    wf.Env.CacheDir,
			"alfred_workflow_data":     wf.Env.DataDir,
		}),
		WithStdout(&bytes.Buffer{}),
	)
}

func TestQuoteArg(t *testing.T) {
	if _, err := os.Stat("/usr/bin/security"); err != nil {
		t.Skip("needs macOS security(1)")
	}
	// show-keychain-info fails on a missing keychain and prints the path it
	// parsed, which shows how security -i read the quoted argument without
	// touching any keychain.
	for _, s := range []string{`plain`, `with space`, `q"uote`, `back\slash`, `'single'`, `$dollar`, `tëst`, `end\`} {
		t.Run(s, func(t *testing.T) {
			path := "/nonexistent/" + s
			cmd := exec.Command("/usr/bin/security", "-i")
			cmd.Stdin = strings.NewReader("show-keychain-info " + quoteArg(path) + "\n")
			out, _ := cmd.CombinedOutput() //nolint:errcheck // it's meant to fail; the message is what's checked
			if !strings.Contains(string(out), path+": ") {
				t.Errorf("security read %q as: %s", path, out)
			}
		})
	}
}
