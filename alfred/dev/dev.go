// Package dev installs a workflow under development straight into Alfred's
// preferences, over the copy Alfred already has, and has Alfred reload it.
//
// It replaces the build, double-click, import loop: build the binary into
// the workflow folder, then call Install with that folder. The cmd/alfred-dev
// command wraps this package for Makefiles.
//
// Install keeps what belongs to the installed copy rather than the source:
// prefs.plist (the user's configuration values) and the values of variables
// marked "Don't Export". It refuses to overwrite an info.plist edited in
// Alfred's editor since the last Install unless Options.Force is set; Pull
// copies such edits back into the source folder instead.
package dev

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ErrNotInstalled is returned by Find when no installed workflow has the
// bundle ID.
var ErrNotInstalled = errors.New("workflow is not installed in Alfred")

// ErrModified is returned by Install when the installed info.plist changed
// since the last Install, usually from editing the workflow in Alfred.
var ErrModified = errors.New("installed info.plist was changed in Alfred since the last dev install")

// ErrLinked is returned by Install when the installed workflow folder is a
// symbolic link. Install would write through it, into the folder it points
// to, which may be the source folder itself.
var ErrLinked = errors.New("installed workflow is a symbolic link")

// keep lists the entries at the root of an installed workflow that Install
// never copies over or removes.
var keep = map[string]bool{"prefs.plist": true}

// Options configures Install and Pull. The zero value uses Alfred's current
// preferences and reloads the workflow.
type Options struct {
	// PrefsDir is the Alfred.alfredpreferences folder. Empty means the one
	// Alfred uses, from PreferencesDir.
	PrefsDir string
	// StateDir holds the hash and backup of each workflow's last installed
	// info.plist. Empty means go-alfredutils/dev in the user cache folder.
	StateDir string
	// Force overwrites an installed info.plist that changed since the last
	// Install.
	Force bool
	// NoReload skips asking Alfred to reload the workflow.
	NoReload bool
}

// Result describes a finished Install.
type Result struct {
	BundleID string
	// Dir is the installed workflow folder.
	Dir string
	// Created is true when the workflow was not installed before.
	Created bool
	// Backup is the copy of the info.plist that was replaced, if any.
	Backup string
	// KeptVars lists the Don't Export variables whose installed values were
	// kept.
	KeptVars []string
	// ReloadErr is why Alfred could not reload the workflow, if it was asked
	// to. The workflow is installed either way.
	ReloadErr error
}

// PreferencesDir returns the Alfred.alfredpreferences folder Alfred is
// using, which may be a synced folder.
func PreferencesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(home, "Library/Application Support/Alfred/prefs.json"))
	if err != nil {
		return "", fmt.Errorf("find Alfred preferences: %w", err)
	}
	var prefs struct {
		Current string `json:"current"`
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return "", fmt.Errorf("find Alfred preferences: %w", err)
	}
	if prefs.Current == "" {
		return "", errors.New("find Alfred preferences: prefs.json has no current preferences")
	}
	return prefs.Current, nil
}

// Find returns the folder in prefsDir/workflows holding the workflow with
// bundleID, or ErrNotInstalled.
func Find(prefsDir, bundleID string) (string, error) {
	workflows := filepath.Join(prefsDir, "workflows")
	entries, err := os.ReadDir(workflows)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range entries {
		dir := filepath.Join(workflows, e.Name())
		// A workflow linked into Alfred's preferences is a symlink, which
		// DirEntry.IsDir doesn't follow.
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		p, _, err := readPlist(filepath.Join(dir, "info.plist"))
		if err != nil {
			continue
		}
		if plistString(p, "bundleid") == bundleID {
			found = append(found, dir)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w: %s", ErrNotInstalled, bundleID)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%d installed workflows have bundle ID %s: %s", len(found), bundleID, strings.Join(found, ", "))
	}
}

// Reload asks Alfred to reload the workflow with bundleID.
func Reload(bundleID string) error {
	out, err := exec.Command("/usr/bin/osascript",
		"-e", "on run argv",
		"-e", `tell application id "com.runningwithcrayons.Alfred" to reload workflow (item 1 of argv)`,
		"-e", "end run",
		bundleID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload %s: %w: %s", bundleID, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install makes the installed copy of the workflow in src match src,
// installing it if Alfred doesn't have it, and reloads it. src is a workflow
// folder holding info.plist and the built binary.
func Install(src string, opts Options) (*Result, error) {
	src, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	srcPlist, _, err := readPlist(filepath.Join(src, "info.plist"))
	if err != nil {
		return nil, err
	}
	res := &Result{BundleID: plistString(srcPlist, "bundleid")}
	if res.BundleID == "" {
		return nil, fmt.Errorf("%s/info.plist has no bundleid", src)
	}
	if opts, err = opts.withDefaults(); err != nil {
		return nil, err
	}
	state := newState(opts.StateDir, res.BundleID)

	installedVars := map[string]string{}
	res.Dir, err = Find(opts.PrefsDir, res.BundleID)
	switch {
	case errors.Is(err, ErrNotInstalled):
		if res.Dir, err = newFolder(opts.PrefsDir); err != nil {
			return nil, err
		}
		res.Created = true
	case err != nil:
		return nil, err
	default:
		if err := checkLinked(src, res.Dir); err != nil {
			return nil, err
		}
		if same, err := sameDir(src, res.Dir); err != nil {
			return nil, err
		} else if same {
			return nil, fmt.Errorf("%s is the installed workflow itself", src)
		}
		p, raw, err := readPlist(filepath.Join(res.Dir, "info.plist"))
		if err != nil {
			return nil, err
		}
		if last, ok := state.hash(); ok && last != hashOf(raw) && !opts.Force {
			return nil, fmt.Errorf("%w: %s", ErrModified, res.Dir)
		}
		if res.Backup, err = state.backup(raw); err != nil {
			return nil, err
		}
		installedVars = plistVars(p)
	}

	if err := syncDir(src, res.Dir); err != nil {
		return nil, fmt.Errorf("copy %s to %s: %w", src, res.Dir, err)
	}

	dstPlist := filepath.Join(res.Dir, "info.plist")
	vars := plistVars(srcPlist)
	for _, name := range plistDontExport(srcPlist) {
		old, ok := installedVars[name]
		if _, defined := vars[name]; ok && defined && old != "" && vars[name] != old {
			vars[name] = old
			res.KeptVars = append(res.KeptVars, name)
		}
	}
	if len(res.KeptVars) > 0 {
		slices.Sort(res.KeptVars)
		if err := writeVars(dstPlist, vars); err != nil {
			return nil, err
		}
	}
	installed, err := os.ReadFile(dstPlist)
	if err != nil {
		return nil, err
	}
	if err := state.setHash(hashOf(installed)); err != nil {
		return nil, err
	}

	if !opts.NoReload {
		res.ReloadErr = Reload(res.BundleID)
	}
	return res, nil
}

// Pull copies the installed info.plist of the workflow in dst into dst,
// with the values of Don't Export variables blanked, and returns the
// installed folder. It keeps edits made in Alfred's editor. Other files,
// such as icons added in Alfred, are not copied.
func Pull(dst string, opts Options) (string, error) {
	bundleID, err := BundleID(dst)
	if err != nil {
		return "", err
	}
	if opts, err = opts.withDefaults(); err != nil {
		return "", err
	}
	dir, err := Find(opts.PrefsDir, bundleID)
	if err != nil {
		return "", err
	}
	if same, err := sameDir(dst, dir); err != nil {
		return "", err
	} else if same {
		return "", fmt.Errorf("%s is the installed workflow itself; there is nothing to pull", dst)
	}
	p, raw, err := readPlist(filepath.Join(dir, "info.plist"))
	if err != nil {
		return "", err
	}
	tmp, err := writeTemp(filepath.Join(dst, "info.plist"), raw, 0o644)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp) //nolint:errcheck // gone after the rename; this only cleans up on failure

	vars, blanked := plistVars(p), false
	for _, name := range plistDontExport(p) {
		if vars[name] != "" {
			vars[name] = ""
			blanked = true
		}
	}
	if blanked {
		if err := writeVars(tmp, vars); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, filepath.Join(dst, "info.plist")); err != nil {
		return "", err
	}
	return dir, newState(opts.StateDir, bundleID).setHash(hashOf(raw))
}

func (o Options) withDefaults() (Options, error) {
	var err error
	if o.PrefsDir == "" {
		if o.PrefsDir, err = PreferencesDir(); err != nil {
			return o, err
		}
	}
	if o.StateDir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return o, err
		}
		o.StateDir = filepath.Join(cache, "go-alfredutils", "dev")
	}
	return o, nil
}

// newFolder returns a new folder path for a workflow, named as Alfred names
// them.
func newFolder(prefsDir string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	id := strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
	return filepath.Join(prefsDir, "workflows", "user.workflow."+id), nil
}

// checkLinked returns ErrLinked when the installed folder dir is a symlink,
// saying where it points and what to do.
func checkLinked(src, dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	target, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrLinked, dir, err)
	}
	if same, err := sameDir(src, dir); err == nil && same {
		return fmt.Errorf("%w: %s links to %s, so Alfred already runs it; replace the link with a copy of the folder to use Install", ErrLinked, dir, target)
	}
	return fmt.Errorf("%w: %s links to %s; replace the link with a copy of the folder to use Install", ErrLinked, dir, target)
}

func sameDir(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// state is where Install remembers a workflow's last installed info.plist.
type state struct{ dir, name string }

func newState(dir, bundleID string) state {
	return state{dir: dir, name: unsafeName.ReplaceAllString(bundleID, "_")}
}

func (s state) hash() (string, bool) {
	data, err := os.ReadFile(filepath.Join(s.dir, s.name+".sha256"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func (s state) setHash(h string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, s.name+".sha256"), []byte(h+"\n"), 0o600)
}

// backup saves data, an installed info.plist, which may hold secret
// variable values, so it is readable only by the user.
func (s state) backup(data []byte) (string, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.dir, s.name+".info.plist.bak")
	return path, os.WriteFile(path, data, 0o600)
}

// syncDir makes dst's contents match src's, except the root entries in keep,
// which it leaves alone.
func syncDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	// Remove what src doesn't have, or has as a different kind of entry.
	err := filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil || rel == "." {
			return err
		}
		if keep[rel] {
			return skip(d)
		}
		fi, err := os.Lstat(filepath.Join(src, rel))
		if err == nil && fi.Mode().Type() == d.Type() {
			return nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		return skip(d)
	})
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		if keep[rel] {
			return skip(d)
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil
		}
	})
}

func skip(d fs.DirEntry) error {
	if d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// copyFile writes a new file and renames it over dst rather than writing
// dst in place: macOS kills a signed binary rewritten in place the next
// time it runs.
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // gone after the rename; this only cleans up on failure
	if _, err := io.Copy(f, in); err != nil {
		f.Close() //nolint:errcheck,gosec // the write error is the one to report
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close() //nolint:errcheck,gosec // the write error is the one to report
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dst)
}

// writeTemp writes data to a new file beside path and returns its name.
func writeTemp(path string, data []byte, perm fs.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return "", err
	}
	_, werr := f.Write(data)
	cerr := f.Chmod(perm)
	if err := errors.Join(werr, cerr, f.Close()); err != nil {
		os.Remove(f.Name()) //nolint:errcheck,gosec // the write error is the one to report
		return "", err
	}
	return f.Name(), nil
}
