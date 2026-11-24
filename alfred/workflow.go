// Package alfred runs Alfred workflows: Script Filter output, cache and data
// stores, background jobs, External Triggers and the Keychain.
package alfred

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"go.deanishe.net/fuzzy"
)

// ErrNotAlfred is returned by New when the environment has no bundle ID,
// which Alfred always sets.
var ErrNotAlfred = errors.New("alfred_workflow_bundleid is not set: not run by Alfred")

const magicPrefix = "workflow:"

// Env is what Alfred tells the workflow about itself through env vars.
type Env struct {
	BundleID      string
	Version       string // empty in Alfred UI scripts
	Name          string
	CacheDir      string
	DataDir       string
	Preferences   string
	Debug         bool // the workflow debugger is open
	AlfredVersion string
}

// Workflow is one run of the workflow binary.
type Workflow struct {
	*Response

	Env      Env
	Cache    *Store
	Data     *Store
	Keychain *Keychain

	lookup     func(string) (string, bool)
	stdout     io.Writer
	args       []string
	exit       func(int)
	maxResults int
	magic      map[string]magic
	noResults  [2]string
	sent       bool
}

type magic struct {
	description string
	runText     string
	fn          func() error
}

// Option configures a Workflow in New.
type Option func(*Workflow)

// WithEnv makes New read env vars from env instead of the process environment.
func WithEnv(env map[string]string) Option {
	return func(wf *Workflow) {
		wf.lookup = func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		}
	}
}

// WithStdout sets where Send writes. The default is os.Stdout.
func WithStdout(w io.Writer) Option { return func(wf *Workflow) { wf.stdout = w } }

// WithArgs sets the arguments Run checks for magic queries. The default is os.Args[1:].
func WithArgs(args ...string) Option { return func(wf *Workflow) { wf.args = args } }

// WithExit replaces os.Exit in FatalError.
func WithExit(fn func(int)) Option { return func(wf *Workflow) { wf.exit = fn } }

// WithMaxResults caps how many items Send writes.
func WithMaxResults(n int) Option { return func(wf *Workflow) { wf.maxResults = n } }

// WithFuzzy sets the options Filter scores items with.
func WithFuzzy(opts ...fuzzy.Option) Option {
	return func(wf *Workflow) { wf.fuzzyOpts = opts }
}

// New reads Alfred's environment. It creates no directories: the stores make
// theirs on the first write.
func New(opts ...Option) (*Workflow, error) {
	wf := &Workflow{
		Response:  NewResponse(),
		lookup:    os.LookupEnv,
		stdout:    os.Stdout,
		args:      os.Args[1:],
		exit:      os.Exit,
		magic:     map[string]magic{},
		noResults: [2]string{"No results found...", "Try a different query?"},
	}
	for _, opt := range opts {
		opt(wf)
	}

	env, err := readEnv(wf.lookup)
	if err != nil {
		return nil, err
	}
	wf.Env = env
	wf.Cache = &Store{dir: env.CacheDir}
	wf.Data = &Store{dir: env.DataDir}
	wf.Keychain = &Keychain{service: env.BundleID}
	return wf, nil
}

func readEnv(lookup func(string) (string, bool)) (Env, error) {
	get := func(k string) string {
		v, _ := lookup(k)
		return v
	}
	env := Env{
		BundleID:      get("alfred_workflow_bundleid"),
		Version:       get("alfred_workflow_version"),
		Name:          get("alfred_workflow_name"),
		CacheDir:      get("alfred_workflow_cache"),
		DataDir:       get("alfred_workflow_data"),
		Preferences:   get("alfred_preferences"),
		Debug:         get("alfred_debug") == "1",
		AlfredVersion: get("alfred_version"),
	}
	if env.BundleID == "" {
		return Env{}, ErrNotAlfred
	}

	if env.CacheDir == "" || env.DataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Env{}, fmt.Errorf("find home directory: %w", err)
		}
		if env.CacheDir == "" {
			env.CacheDir = filepath.Join(home, "Library/Caches/com.runningwithcrayons.Alfred/Workflow Data", env.BundleID)
		}
		if env.DataDir == "" {
			env.DataDir = filepath.Join(home, "Library/Application Support/Alfred/Workflow Data", env.BundleID)
		}
	}
	return env, nil
}

// Lookup returns a workflow variable.
func (wf *Workflow) Lookup(name string) (string, bool) { return wf.lookup(name) }

// Run calls fn. A panic in fn becomes a FatalError. A query starting with
// "workflow:" runs the matching magic action instead of fn.
func (wf *Workflow) Run(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic: %v\n%s", r, debug.Stack())
			wf.FatalError(fmt.Errorf("%v", r))
		}
	}()
	if wf.runMagic() {
		return
	}
	fn()
}

// AddMagic registers an action that runs when the Script Filter query is
// "workflow:<keyword>". runText is shown while it runs. A query of just
// "workflow:" lists the registered actions.
func (wf *Workflow) AddMagic(keyword, description, runText string, fn func() error) {
	wf.magic[keyword] = magic{description: description, runText: runText, fn: fn}
}

func (wf *Workflow) runMagic() bool {
	for _, arg := range wf.args {
		query, ok := strings.CutPrefix(strings.TrimSpace(arg), magicPrefix)
		if !ok {
			continue
		}
		if m, ok := wf.magic[query]; ok {
			wf.NewItem(m.runText).Icon(IconInfo)
			wf.Send()
			if err := m.fn(); err != nil {
				log.Printf("magic action %q: %v", query, err)
			}
			return true
		}

		keywords := make([]string, 0, len(wf.magic))
		for kw := range wf.magic {
			keywords = append(keywords, kw)
		}
		sort.Strings(keywords)
		for _, kw := range keywords {
			wf.NewItem(kw).
				Subtitle(wf.magic[kw].description).
				Autocomplete(magicPrefix + kw).
				Match(kw + " " + wf.magic[kw].description).
				Icon(IconInfo)
		}
		wf.Filter(query)
		wf.NoResults("No matching action", "Try another query?")
		wf.Send()
		return true
	}
	return false
}

// NoResults sets the row Send adds when there are no items.
func (wf *Workflow) NoResults(title, subtitle string) {
	wf.noResults = [2]string{title, subtitle}
}

// Send writes the Script Filter JSON. Only the first call writes anything.
func (wf *Workflow) Send() {
	if wf.sent {
		log.Print("feedback already sent")
		return
	}
	wf.sent = true

	if wf.IsEmpty() {
		wf.NewItem(wf.noResults[0]).Subtitle(wf.noResults[1]).Icon(IconInfo)
	}
	if wf.maxResults > 0 && len(wf.items) > wf.maxResults {
		wf.items = wf.items[:wf.maxResults]
	}
	if err := json.NewEncoder(wf.stdout).Encode(wf.Response); err != nil {
		log.Printf("write feedback: %v", err)
	}
}

// FatalError replaces the items with one error row, sends it, and exits 1.
func (wf *Workflow) FatalError(err error) {
	log.Printf("error: %v", err)
	if !wf.sent {
		opts := wf.fuzzyOpts
		wf.Response = NewResponse()
		wf.fuzzyOpts = opts
		wf.NewItem(err.Error()).Largetype(err.Error()).Icon(IconError)
		wf.Send()
	}
	wf.exit(1)
}
