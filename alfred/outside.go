package alfred

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/rwilgaard/go-alfredutils/v2/internal/plist"
)

// WithWorkflowDir makes New read the workflow's bundle ID, variables and
// configuration from dir/info.plist (and dir/prefs.plist) when Alfred didn't
// set them, as it does for a run from a terminal. The env vars New would
// otherwise read still take precedence.
//
// Without this option, and without WithEnv, New looks for info.plist next to
// the executable, then in the current directory, then in ./workflow.
func WithWorkflowDir(dir string) Option {
	return func(wf *Workflow) { wf.workflowDir = dir }
}

// outsideCandidates are the folders New searches for info.plist when the
// binary was not run by Alfred.
func outsideCandidates() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			dirs = append(dirs, filepath.Dir(exe))
		}
	}
	return append(dirs, ".", "workflow")
}

// outsideLookup returns a lookup that falls back to the values Alfred would
// set from the workflow folder holding info.plist. It returns nil when no
// folder is given and none of the candidates holds an info.plist.
func (wf *Workflow) outsideLookup(base func(string) (string, bool)) (func(string) (string, bool), error) {
	dir := wf.workflowDir
	if dir == "" {
		for _, c := range outsideCandidates() {
			if _, err := os.Stat(filepath.Join(c, "info.plist")); err == nil {
				dir = c
				break
			}
		}
		if dir == "" {
			return nil, nil
		}
	}
	vars, err := outsideVars(dir)
	if err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	log.Printf("not run by Alfred: using %s", filepath.Join(dir, "info.plist"))
	return func(k string) (string, bool) {
		if v, ok := base(k); ok {
			return v, true
		}
		v, ok := vars[k]
		return v, ok
	}, nil
}

// outsideVars returns the env vars Alfred would give the workflow in dir:
// its bundle ID, name and version, and its variables, with User
// Configuration values from prefs.plist over their defaults.
func outsideVars(dir string) (map[string]string, error) {
	info, _, err := plist.Read(filepath.Join(dir, "info.plist"))
	if err != nil {
		return nil, fmt.Errorf("read workflow outside Alfred: %w", err)
	}
	bundleID, _ := info["bundleid"].(string)
	if bundleID == "" {
		return nil, fmt.Errorf("read workflow outside Alfred: %s has no bundleid", filepath.Join(dir, "info.plist"))
	}
	vars := map[string]string{}
	configs, _ := info["userconfigurationconfig"].([]any)
	for _, c := range configs {
		c, _ := c.(map[string]any)
		name, _ := c["variable"].(string)
		config, _ := c["config"].(map[string]any)
		if v, ok := envValue(config["default"]); ok && name != "" {
			vars[name] = v
		}
	}
	variables, _ := info["variables"].(map[string]any)
	for k, v := range variables {
		if s, ok := envValue(v); ok {
			vars[k] = s
		}
	}
	prefs, _, err := plist.Read(filepath.Join(dir, "prefs.plist"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read workflow outside Alfred: %w", err)
	}
	for k, v := range prefs {
		if s, ok := envValue(v); ok {
			vars[k] = s
		}
	}
	vars["alfred_workflow_bundleid"] = bundleID
	if name, ok := info["name"].(string); ok {
		vars["alfred_workflow_name"] = name
	}
	if version, ok := info["version"].(string); ok {
		vars["alfred_workflow_version"] = version
	}
	return vars, nil
}

// envValue is v as Alfred passes it in an env var: booleans become "1" or
// "0".
func envValue(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case bool:
		if v {
			return "1", true
		}
		return "0", true
	default:
		return "", false
	}
}
