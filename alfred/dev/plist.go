package dev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/rwilgaard/go-alfredutils/v2/internal/plist"
)

const plutil = "/usr/bin/plutil"

// readPlist decodes the property list at path. See plist.Read.
func readPlist(path string) (map[string]any, []byte, error) { return plist.Read(path) }

func plistString(p map[string]any, key string) string {
	s, _ := p[key].(string)
	return s
}

func plistVars(p map[string]any) map[string]string {
	m, _ := p["variables"].(map[string]any)
	vars := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			vars[k] = s
		}
	}
	return vars
}

func plistDontExport(p map[string]any) []string {
	a, _ := p["variablesdontexport"].([]any)
	var names []string
	for _, v := range a {
		if s, ok := v.(string); ok {
			names = append(names, s)
		}
	}
	return names
}

// writeVars replaces the variables dict of the plist at path.
func writeVars(path string, vars map[string]string) error {
	js, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	out, err := exec.Command(plutil, "-replace", "variables", "-json", string(js), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("write variables to %s: %w: %s", path, err, bytes.TrimSpace(out))
	}
	return nil
}

// BundleID returns the bundle ID in dir/info.plist.
func BundleID(dir string) (string, error) {
	path := filepath.Join(dir, "info.plist")
	p, _, err := readPlist(path)
	if err != nil {
		return "", err
	}
	id := plistString(p, "bundleid")
	if id == "" {
		return "", errors.New(path + " has no bundleid")
	}
	return id, nil
}
