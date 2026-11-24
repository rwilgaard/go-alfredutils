// Command alfred-dev installs a workflow under development into Alfred and
// reloads it, without exporting and importing an .alfredworkflow.
//
// Run it from a workflow repository after building the binary into the
// workflow folder:
//
//	go run github.com/rwilgaard/go-alfredutils/v2/cmd/alfred-dev install
//
// See package dev for what it keeps from the installed copy.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rwilgaard/go-alfredutils/v2/alfred/dev"
)

const usage = `usage: alfred-dev <command> [-dir folder]

commands:
  install [-force] [-no-reload]
          copy the workflow folder over Alfred's installed copy and reload it
  pull    copy the installed info.plist, with edits made in Alfred, back
          into the workflow folder; Don't Export values are blanked
  reload  ask Alfred to reload the workflow
  path    print the installed workflow folder

-dir is the workflow folder holding info.plist (default "workflow").
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		say(stderr, "%s", usage)
		return 2
	}
	cmd := args[0]
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { say(stderr, "%s", usage) }
	dir := flags.String("dir", "workflow", "workflow folder holding info.plist")
	force := new(bool)
	noReload := new(bool)
	if cmd == "install" {
		force = flags.Bool("force", false, "overwrite an info.plist changed in Alfred")
		noReload = flags.Bool("no-reload", false, "don't ask Alfred to reload the workflow")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		say(stderr, "error: unexpected argument %q\n", flags.Arg(0))
		return 2
	}

	var err error
	switch cmd {
	case "install":
		err = install(*dir, dev.Options{Force: *force, NoReload: *noReload}, stdout, stderr)
	case "pull":
		var installed string
		if installed, err = dev.Pull(*dir, dev.Options{}); err == nil {
			say(stdout, "Copied %s/info.plist to %s/info.plist\n", installed, *dir)
		}
	case "reload":
		var id string
		if id, err = dev.BundleID(*dir); err == nil {
			err = dev.Reload(id)
		}
	case "path":
		var installed string
		if installed, err = installedDir(*dir); err == nil {
			say(stdout, "%v\n", installed)
		}
	default:
		say(stderr, "%s", usage)
		return 2
	}
	if err != nil {
		say(stderr, "error: %v\n", err)
		if errors.Is(err, dev.ErrModified) {
			say(stderr, "run 'alfred-dev pull' to keep Alfred's version in %s, or 'alfred-dev install -force' to discard it\n", *dir)
		}
		return 1
	}
	return 0
}

func install(dir string, opts dev.Options, stdout, stderr io.Writer) error {
	res, err := dev.Install(dir, opts)
	if err != nil {
		return err
	}
	verb := "Updated"
	if res.Created {
		verb = "Installed"
	}
	say(stdout, "%s %s in %s\n", verb, res.BundleID, res.Dir)
	if len(res.KeptVars) > 0 {
		say(stdout, "Kept installed values of %s\n", strings.Join(res.KeptVars, ", "))
	}
	if res.ReloadErr != nil {
		say(stderr, "warning: %v\nreload the workflow in Alfred Preferences\n", res.ReloadErr)
	}
	return nil
}

func installedDir(dir string) (string, error) {
	id, err := dev.BundleID(dir)
	if err != nil {
		return "", err
	}
	prefs, err := dev.PreferencesDir()
	if err != nil {
		return "", err
	}
	return dev.Find(prefs, id)
}

// say prints to a terminal, where a failed write has nowhere to be reported.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
