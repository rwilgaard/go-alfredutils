package alfred_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
)

const outsideInfo = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>bundleid</key>
	<string>com.example.outside</string>
	<key>name</key>
	<string>Outside</string>
	<key>version</key>
	<string>1.2.3</string>
	<key>userconfigurationconfig</key>
	<array>
		<dict>
			<key>type</key><string>textfield</string>
			<key>variable</key><string>url</string>
			<key>config</key><dict><key>default</key><string>https://default.example</string></dict>
		</dict>
		<dict>
			<key>type</key><string>checkbox</string>
			<key>variable</key><string>check_for_updates</string>
			<key>config</key><dict><key>default</key><true/></dict>
		</dict>
		<dict>
			<key>type</key><string>textfield</string>
			<key>variable</key><string>cache_age</string>
			<key>config</key><dict><key>default</key><string>360</string></dict>
		</dict>
	</array>
	<key>variables</key>
	<dict>
		<key>limit</key><string>10</string>
	</dict>
</dict>
</plist>
`

const outsidePrefs = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>url</key><string>https://mine.example</string>
	<key>check_for_updates</key><false/>
</dict>
</plist>
`

func workflowDir(t *testing.T, prefs bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "info.plist"), []byte(outsideInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	if prefs {
		if err := os.WriteFile(filepath.Join(dir, "prefs.plist"), []byte(outsidePrefs), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOutsideAlfred(t *testing.T) {
	dir := workflowDir(t, true)
	// cache_age set in the environment wins over everything in the folder.
	wf, err := alfred.New(alfred.WithEnv(map[string]string{"cache_age": "5"}), alfred.WithWorkflowDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !wf.Env.Outside || wf.Env.BundleID != "com.example.outside" || wf.Env.Name != "Outside" || wf.Env.Version != "" {
		t.Errorf("Env = %+v", wf.Env)
	}
	if !strings.HasSuffix(wf.Env.CacheDir, filepath.Join("Workflow Data", "com.example.outside")) {
		t.Errorf("CacheDir = %q, want Alfred's cache folder for the bundle ID", wf.Env.CacheDir)
	}
	for name, want := range map[string]string{
		"url":               "https://mine.example", // prefs.plist over the default
		"check_for_updates": "0",                    // a checkbox, as Alfred passes it
		"cache_age":         "5",                    // the environment over the default
		"limit":             "10",                   // a workflow variable
	} {
		if got, _ := wf.Lookup(name); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOutsideAlfredDefaults(t *testing.T) {
	wf, err := alfred.New(alfred.WithEnv(nil), alfred.WithWorkflowDir(workflowDir(t, false)))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"url": "https://default.example", "check_for_updates": "1"} {
		if got, _ := wf.Lookup(name); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOutsideAlfredFindsWorkflowFolder(t *testing.T) {
	if _, ok := os.LookupEnv("alfred_workflow_bundleid"); ok {
		t.Skip("running under Alfred")
	}
	root := t.TempDir()
	if err := os.Rename(workflowDir(t, false), filepath.Join(root, "workflow")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	wf, err := alfred.New()
	if err != nil {
		t.Fatal(err)
	}
	if !wf.Env.Outside || wf.Env.BundleID != "com.example.outside" {
		t.Errorf("Env = %+v, want it read from ./workflow/info.plist", wf.Env)
	}
}

func TestInsideAlfredIgnoresWorkflowDir(t *testing.T) {
	wf, err := alfred.New(alfred.WithEnv(map[string]string{"alfred_workflow_bundleid": "com.example.real"}), alfred.WithWorkflowDir(workflowDir(t, true)))
	if err != nil {
		t.Fatal(err)
	}
	if wf.Env.Outside || wf.Env.BundleID != "com.example.real" {
		t.Errorf("Env = %+v, want Alfred's values", wf.Env)
	}
	if _, ok := wf.Lookup("url"); ok {
		t.Error("info.plist values used although Alfred set the environment")
	}
}

func TestOutsideAlfredErrors(t *testing.T) {
	if _, err := alfred.New(alfred.WithEnv(nil)); !errors.Is(err, alfred.ErrNotAlfred) {
		t.Errorf("WithEnv without bundle ID: err = %v, want ErrNotAlfred", err)
	}
	if _, err := alfred.New(alfred.WithEnv(nil), alfred.WithWorkflowDir(t.TempDir())); err == nil || errors.Is(err, alfred.ErrNotAlfred) {
		t.Errorf("WithWorkflowDir without info.plist: err = %v, want a read error", err)
	}
}
