package dev

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const bundle = "com.example.dev-test"

// infoPlist returns an info.plist with the bundle ID, variables and Don't
// Export names given.
func infoPlist(bundleID string, vars map[string]string, dontExport ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>bundleid</key>
	<string>` + bundleID + `</string>
	<key>disabled</key>
	<false/>
	<key>variables</key>
	<dict>
`)
	for k, v := range vars {
		fmt.Fprintf(&b, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", k, v)
	}
	b.WriteString("\t</dict>\n\t<key>variablesdontexport</key>\n\t<array>\n")
	for _, n := range dontExport {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", n)
	}
	b.WriteString("\t</array>\n</dict>\n</plist>\n")
	return b.String()
}

func write(t *testing.T, path, data string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

type env struct {
	src, prefs string
	opts       Options
}

// setup makes a source workflow folder and an empty Alfred preferences
// folder.
func setup(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	e := env{src: filepath.Join(root, "workflow"), prefs: filepath.Join(root, "Alfred.alfredpreferences")}
	e.opts = Options{PrefsDir: e.prefs, StateDir: filepath.Join(root, "state"), NoReload: true}
	write(t, filepath.Join(e.src, "info.plist"), infoPlist(bundle, map[string]string{"TOKEN": "", "LIMIT": "10"}, "TOKEN"), 0o644)
	write(t, filepath.Join(e.src, "bin"), "binary v1", 0o755)
	write(t, filepath.Join(e.src, "icons", "a.png"), "a", 0o644)
	if err := os.MkdirAll(filepath.Join(e.prefs, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

// installed adds an installed workflow folder holding info.plist.
func (e env) installed(t *testing.T, name, plist string) string {
	t.Helper()
	dir := filepath.Join(e.prefs, "workflows", name)
	write(t, filepath.Join(dir, "info.plist"), plist, 0o644)
	return dir
}

func TestInstallNew(t *testing.T) {
	e := setup(t)
	res, err := Install(e.src, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.BundleID != bundle || res.Backup != "" {
		t.Errorf("result = %+v, want a new install of %s without backup", res, bundle)
	}
	if !strings.HasPrefix(filepath.Base(res.Dir), "user.workflow.") {
		t.Errorf("folder = %s, want user.workflow.<uuid>", res.Dir)
	}
	if got, err := Find(e.prefs, bundle); err != nil || got != res.Dir {
		t.Errorf("Find = %q, %v; want %q", got, err, res.Dir)
	}
	if got := read(t, filepath.Join(res.Dir, "icons", "a.png")); got != "a" {
		t.Errorf("icons/a.png = %q", got)
	}
	fi, err := os.Stat(filepath.Join(res.Dir, "bin"))
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("bin mode = %v, %v; want 0755", fi.Mode().Perm(), err)
	}
}

func TestInstallUpdate(t *testing.T) {
	e := setup(t)
	dir := e.installed(t, "user.workflow.OLD", infoPlist(bundle, map[string]string{"TOKEN": "secret", "LIMIT": "5"}, "TOKEN"))
	write(t, filepath.Join(dir, "prefs.plist"), "user config", 0o644)
	write(t, filepath.Join(dir, "bin"), "binary v0", 0o755)
	write(t, filepath.Join(dir, "stale.txt"), "x", 0o644)
	write(t, filepath.Join(dir, "old", "x.png"), "x", 0o644)
	write(t, filepath.Join(dir, "icons"), "a file where src has a folder", 0o644)

	res, err := Install(e.src, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.Dir != dir {
		t.Errorf("result = %+v, want an update of %s", res, dir)
	}
	if got := read(t, filepath.Join(dir, "bin")); got != "binary v1" {
		t.Errorf("bin = %q, want the new build", got)
	}
	if got := read(t, filepath.Join(dir, "prefs.plist")); got != "user config" {
		t.Errorf("prefs.plist = %q, want it kept", got)
	}
	for _, gone := range []string{"stale.txt", "old"} {
		if exists(filepath.Join(dir, gone)) {
			t.Errorf("%s still installed", gone)
		}
	}
	if got := read(t, filepath.Join(dir, "icons", "a.png")); got != "a" {
		t.Errorf("icons/a.png = %q", got)
	}
	p, _, err := readPlist(filepath.Join(dir, "info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	// TOKEN is Don't Export, so its installed value stays; LIMIT comes from src.
	if got, want := plistVars(p), map[string]string{"TOKEN": "secret", "LIMIT": "10"}; !reflect.DeepEqual(got, want) {
		t.Errorf("variables = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(res.KeptVars, []string{"TOKEN"}) {
		t.Errorf("KeptVars = %v", res.KeptVars)
	}
	if !strings.Contains(read(t, res.Backup), "<string>secret</string>") {
		t.Errorf("backup %s doesn't hold the replaced info.plist", res.Backup)
	}
	if fi, err := os.Stat(res.Backup); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	if strings.Contains(read(t, filepath.Join(e.src, "info.plist")), "secret") {
		t.Error("secret written to the source info.plist")
	}
}

func TestInstallModified(t *testing.T) {
	e := setup(t)
	res, err := Install(e.src, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	// Reinstalling an unchanged install is fine.
	if _, err := Install(e.src, e.opts); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	edited := infoPlist(bundle, map[string]string{"TOKEN": "", "LIMIT": "99"}, "TOKEN")
	write(t, filepath.Join(res.Dir, "info.plist"), edited, 0o644)
	if _, err := Install(e.src, e.opts); !errors.Is(err, ErrModified) {
		t.Fatalf("Install after editing in Alfred: err = %v, want ErrModified", err)
	}
	if got := read(t, filepath.Join(res.Dir, "info.plist")); got != edited {
		t.Error("refused Install still changed info.plist")
	}

	force := e.opts
	force.Force = true
	if _, err := Install(e.src, force); err != nil {
		t.Fatalf("forced Install: %v", err)
	}
	if strings.Contains(read(t, filepath.Join(res.Dir, "info.plist")), "99") {
		t.Error("forced Install kept Alfred's edit")
	}
}

func TestPull(t *testing.T) {
	e := setup(t)
	res, err := Install(e.src, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	edited := infoPlist(bundle, map[string]string{"TOKEN": "secret", "LIMIT": "99"}, "TOKEN")
	write(t, filepath.Join(res.Dir, "info.plist"), edited, 0o644)

	dir, err := Pull(e.src, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if dir != res.Dir {
		t.Errorf("Pull = %s, want %s", dir, res.Dir)
	}
	p, _, err := readPlist(filepath.Join(e.src, "info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plistVars(p), map[string]string{"TOKEN": "", "LIMIT": "99"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pulled variables = %v, want %v", got, want)
	}
	if matches, _ := filepath.Glob(filepath.Join(e.src, ".info.plist.*")); len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
	// The pulled version is now the known install, so Install goes ahead
	// and keeps the secret.
	res, err = Install(e.src, e.opts)
	if err != nil {
		t.Fatalf("Install after Pull: %v", err)
	}
	if !strings.Contains(read(t, filepath.Join(res.Dir, "info.plist")), "secret") {
		t.Error("Install after Pull lost the Don't Export value")
	}
}

func TestFind(t *testing.T) {
	e := setup(t)
	if _, err := Find(e.prefs, bundle); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("empty prefs: err = %v, want ErrNotInstalled", err)
	}
	e.installed(t, "user.workflow.OTHER", infoPlist("com.example.other", nil))
	dir := e.installed(t, "user.workflow.A", infoPlist(bundle, nil))
	if out, err := exec.Command(plutil, "-convert", "binary1", filepath.Join(dir, "info.plist")).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v: %s", err, out)
	}
	if got, err := Find(e.prefs, bundle); err != nil || got != dir {
		t.Errorf("Find binary plist = %q, %v; want %q", got, err, dir)
	}
	e.installed(t, "user.workflow.B", infoPlist(bundle, nil))
	if _, err := Find(e.prefs, bundle); err == nil || !strings.Contains(err.Error(), "2 installed workflows") {
		t.Errorf("duplicate bundle IDs: err = %v", err)
	}
}

// linked adds an installed workflow that is a symlink to target.
func (e env) linked(t *testing.T, name, target string) string {
	t.Helper()
	dir := filepath.Join(e.prefs, "workflows", name)
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFindSymlink(t *testing.T) {
	e := setup(t)
	dir := e.linked(t, "user.workflow.LINK", e.src)
	if got, err := Find(e.prefs, bundle); err != nil || got != dir {
		t.Errorf("Find = %q, %v; want %q", got, err, dir)
	}
	// A dangling link or a link to a file isn't a workflow.
	e.linked(t, "user.workflow.DANGLING", filepath.Join(e.src, "missing"))
	e.linked(t, "user.workflow.FILE", filepath.Join(e.src, "bin"))
	if got, err := Find(e.prefs, bundle); err != nil || got != dir {
		t.Errorf("Find with bad links = %q, %v; want %q", got, err, dir)
	}
}

func TestInstallLinked(t *testing.T) {
	t.Run("to source", func(t *testing.T) {
		e := setup(t)
		e.linked(t, "user.workflow.LINK", e.src)
		before := read(t, filepath.Join(e.src, "bin"))
		_, err := Install(e.src, e.opts)
		if !errors.Is(err, ErrLinked) || !strings.Contains(err.Error(), "already runs it") {
			t.Fatalf("err = %v, want ErrLinked saying Alfred already runs it", err)
		}
		if got := read(t, filepath.Join(e.src, "bin")); got != before {
			t.Errorf("source bin = %q, want %q", got, before)
		}
		entries, err := os.ReadDir(filepath.Join(e.prefs, "workflows"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("%d installed workflows, want 1: Install created a duplicate", len(entries))
		}
	})
	t.Run("elsewhere", func(t *testing.T) {
		e := setup(t)
		other := filepath.Join(t.TempDir(), "other")
		write(t, filepath.Join(other, "info.plist"), infoPlist(bundle, nil), 0o644)
		write(t, filepath.Join(other, "keepme"), "x", 0o644)
		e.linked(t, "user.workflow.LINK", other)
		if _, err := Install(e.src, e.opts); !errors.Is(err, ErrLinked) {
			t.Fatalf("err = %v, want ErrLinked", err)
		}
		if !exists(filepath.Join(other, "keepme")) || exists(filepath.Join(other, "bin")) {
			t.Error("Install wrote into the linked folder")
		}
	})
}

func TestPullLinkedToSource(t *testing.T) {
	e := setup(t)
	e.linked(t, "user.workflow.LINK", e.src)
	if _, err := Pull(e.src, e.opts); err == nil || !strings.Contains(err.Error(), "nothing to pull") {
		t.Errorf("err = %v, want nothing to pull", err)
	}
}

func TestInstallIntoItself(t *testing.T) {
	e := setup(t)
	res, err := Install(e.src, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(res.Dir, e.opts); err == nil {
		t.Error("installing the installed folder over itself succeeded")
	}
}
