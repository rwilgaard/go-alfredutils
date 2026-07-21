package alfredutils

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	aw "github.com/deanishe/awgo"
	"github.com/deanishe/awgo/keychain"
)

type magicAuth struct {
	Workflow *aw.Workflow
	Account  string
}

func (a magicAuth) Keyword() string     { return "clearauth" }
func (a magicAuth) Description() string { return "Clear credentials." }
func (a magicAuth) RunText() string     { return "Credentials cleared!" }
func (a magicAuth) Run() error {
	err := ClearAuth(a.Workflow, a.Account)
	if errors.Is(err, keychain.ErrNotFound) {
		return nil
	}
	return err
}

func ClearAuth(wf *aw.Workflow, keychainAccount string) error {
	return wf.Keychain.Delete(keychainAccount)
}

func AddClearAuthMagic(wf *aw.Workflow, keychainAccount string) {
	wf.Configure(aw.AddMagic(magicAuth{
		Workflow: wf,
		Account:  keychainAccount,
	}))
}

func InitWorkflow(wf *aw.Workflow, cfg any) error {
	wf.Args()
	return wf.Config.To(cfg)
}

func CheckForUpdates(wf *aw.Workflow) error {
	updateJobName := "update"
	if wf.UpdateCheckDue() && !wf.IsRunning(updateJobName) {
		log.Println("Running update check in background...")
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		cmd := exec.Command(exe, updateJobName)
		if err := wf.RunInBackground(updateJobName, cmd); err != nil {
			return fmt.Errorf("error starting update check: %w", err)
		}
	}

	if wf.UpdateAvailable() {
		wf.NewItem("Update Available!").
			Subtitle("Press ⏎ to install").
			Autocomplete("workflow:update").
			Valid(false).
			Icon(aw.IconInfo)
	}

	return nil
}

func HandleFeedback(wf *aw.Workflow) {
	if wf.IsEmpty() {
		wf.NewItem("No results found...").
			Subtitle("Try a different query?").
			Icon(aw.IconInfo)
	}
	wf.SendFeedback()
}

// LoadCache loads cached JSON into out. Returns nil without modifying out if
// the cache file does not exist yet.
func LoadCache(wf *aw.Workflow, name string, out any) error {
	if err := wf.Cache.LoadJSON(name, out); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

// RefreshCache starts a background job to refresh the named cache if it has
// expired. Adds a "Refreshing cache…" item if the cache file does not yet
// exist. An optional icon overrides the default IconInfo on that item. The
// caller is responsible for calling HandleFeedback afterward.
func RefreshCache(wf *aw.Workflow, name string, maxAge time.Duration, cmdArgs []string, icon ...*aw.Icon) error {
	if !wf.Cache.Expired(name, maxAge) {
		return nil
	}

	if !wf.IsRunning(name) {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		cmd := exec.Command(exe, cmdArgs...)
		if err := wf.RunInBackground(name, cmd); err != nil {
			return err
		}
	} else {
		log.Printf("%s job already running.", name)
	}

	wf.Rerun(2)

	if !wf.Cache.Exists(name) {
		cacheIcon := aw.IconInfo
		if len(icon) > 0 && icon[0] != nil {
			cacheIcon = icon[0]
		}
		wf.NewItem("Refreshing cache…").Icon(cacheIcon)
	}

	return nil
}

// HandleAuthentication checks for stored credentials and returns true if found.
// If credentials are missing or a keychain error occurs, it adds an appropriate
// item and calls HandleFeedback. Callers must return immediately when this
// returns false.
func HandleAuthentication(wf *aw.Workflow, keychainAccount string) bool {
	_, err := wf.Keychain.Get(keychainAccount)
	if err == nil {
		return true
	}
	if errors.Is(err, keychain.ErrNotFound) {
		wf.NewItem("You're not logged in.").
			Subtitle("Press ⏎ to authenticate").
			Icon(aw.IconInfo).
			Arg("auth").
			Valid(true)
	} else {
		wf.NewItem("Keychain error.").
			Subtitle(err.Error()).
			Icon(aw.IconWarning)
	}
	HandleFeedback(wf)
	return false
}
