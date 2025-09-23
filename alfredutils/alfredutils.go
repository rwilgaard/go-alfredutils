package alfredutils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	aw "github.com/deanishe/awgo"
)

type magicAuth struct {
    Workflow *aw.Workflow
    Account  string
}

func (a magicAuth) Keyword() string     { return "clearauth" }
func (a magicAuth) Description() string { return "Clear credentials." }
func (a magicAuth) RunText() string     { return "Credentials cleared!" }
func (a magicAuth) Run() error          { return ClearAuth(a.Workflow, a.Account) }

func ClearAuth(wf *aw.Workflow, keychainAccount string) error {
    if err := wf.Keychain.Delete(keychainAccount); err != nil {
        return err
    }
    return nil
}

func AddClearAuthMagic(wf *aw.Workflow, keychainAccount string) {
    wf.Configure(aw.AddMagic(magicAuth{
        Workflow: wf,
        Account:  keychainAccount,
    }))
}

func InitWorkflow(wf *aw.Workflow, cfg any) error {
    wf.Args()
    if err := wf.Config.To(cfg); err != nil {
        return err
    }
    return nil
}

func CheckForUpdates(wf *aw.Workflow) error {
    updateJobName := "update"
    if wf.UpdateCheckDue() && !wf.IsRunning(updateJobName) {
        log.Println("Running update check in background...")
        cmd := exec.Command(os.Args[0], updateJobName)
        if err := wf.RunInBackground(updateJobName, cmd); err != nil {
            return fmt.Errorf("Error starting update check: %s", err)
        }
    }

    if wf.UpdateAvailable() {
        wf.Configure(aw.SuppressUIDs(true))
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

func LoadCache(wf *aw.Workflow, name string, out any) error {
    if wf.Cache.Exists(name) {
        if err := wf.Cache.LoadJSON(name, out); err != nil {
            return err
        }
    }
    return nil
}

func RefreshCache(wf *aw.Workflow, name string, maxAge time.Duration, cmdArgs []string) error {
    cacheJobName := "cache"
    if wf.Cache.Expired(name, maxAge) {
        wf.Rerun(2)
        if !wf.IsRunning(cacheJobName) {
            cmd := exec.Command(os.Args[0], cmdArgs...)
            if err := wf.RunInBackground(cacheJobName, cmd); err != nil {
                return err
            }
        } else {
            log.Printf("%s job already running.", cacheJobName)
        }

        var cache []any
        err := LoadCache(wf, name, &cache)
        if err != nil {
            return err
        }

        if len(cache) == 0 {
            wf.NewItem("Refreshing cache…").
                Icon(aw.IconInfo)
            HandleFeedback(wf)
        }
    }
    return nil
}

func HandleAuthentication(wf *aw.Workflow, keychainAccount string) (ok bool) {
    _, err := wf.Keychain.Get(keychainAccount)
    if err != nil {
        wf.NewItem("You're not logged in.").
            Subtitle("Press ⏎ to authenticate").
            Icon(aw.IconInfo).
            Arg("auth").
            Valid(true)
        HandleFeedback(wf)
		return false
    }
	return true
}
