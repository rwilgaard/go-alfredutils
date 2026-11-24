package alfred

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotFound is returned when the Keychain has no entry for an account.
var ErrNotFound = errors.New("keychain item not found")

// Keychain stores secrets as generic passwords whose service is the bundle
// ID, the same entries awgo writes.
type Keychain struct{ service string }

// security(1) exit codes.
const (
	secNotFound  = 44
	secDuplicate = 45
)

func (k *Keychain) Get(account string) (string, error) {
	// -g prints the password on stderr in a form that also covers non-ASCII
	// secrets, which -w would print as bare hex.
	out, err := k.run("find-generic-password", account, "-g")
	if err != nil {
		return "", err
	}
	return parsePassword(out)
}

// Set stores secret for account, replacing an existing entry.
func (k *Keychain) Set(account, secret string) error {
	_, err := k.run("add-generic-password", account, "-U", "-w", secret)
	return err
}

func (k *Keychain) Delete(account string) error {
	_, err := k.run("delete-generic-password", account)
	return err
}

func (k *Keychain) run(command, account string, args ...string) (string, error) {
	args = append([]string{command, "-s", k.service, "-a", account}, args...)
	cmd := exec.Command("/usr/bin/security", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		switch cmd.ProcessState.ExitCode() {
		case secNotFound:
			return "", ErrNotFound
		case secDuplicate:
			return "", fmt.Errorf("keychain item for %s already exists", account)
		}
		return "", fmt.Errorf("security %s: %w: %s", command, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stderr.String()), nil
}

// parsePassword reads security -g output: `password: "secret"` for ASCII,
// or `password: 0x<hex>  "<escaped>"` otherwise.
func parsePassword(out string) (string, error) {
	_, s, ok := strings.Cut(out, "password: ")
	if !ok {
		return "", ErrNotFound
	}
	if strings.HasPrefix(s, `"`) {
		return strings.TrimSuffix(s[1:], `"`), nil
	}
	if hexStr, ok := strings.CutPrefix(s, "0x"); ok {
		hexStr, _, _ = strings.Cut(hexStr, " ")
		b, err := hex.DecodeString(hexStr)
		if err != nil {
			return "", fmt.Errorf("decode keychain password: %w", err)
		}
		return string(b), nil
	}
	return "", ErrNotFound
}
