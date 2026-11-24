// Package update checks a workflow's GitHub releases for a newer
// .alfredworkflow and installs it.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
)

const (
	stateKey = "_update/latest.json"
	// enabledVar is the workflow variable that turns update checks off when
	// "0" or "false", typically a User Configuration checkbox.
	enabledVar = "check_for_updates"
	jobName    = "update"
	interval   = 24 * time.Hour
)

// Updater checks one GitHub repo for releases.
type Updater struct {
	wf     *alfred.Workflow
	repo   string
	apiURL string
	client *http.Client
}

type release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// GitHub returns an Updater for repo ("owner/name") and registers the
// "workflow:update" magic query that installs the update. Call it before
// wf.Run, which handles that query.
func GitHub(wf *alfred.Workflow, repo string) *Updater {
	u := &Updater{
		wf:     wf,
		repo:   repo,
		apiURL: "https://api.github.com",
		client: &http.Client{Timeout: 30 * time.Second},
	}
	if wf.Env.Version != "" {
		wf.AddMagic("update", "Install the latest version", "Downloading update…", func() error {
			return u.Install(context.Background())
		})
	}
	return u
}

// Enabled reports whether update checks are on: they are unless the
// check_for_updates variable is false. The "workflow:update" query works
// either way.
func (u *Updater) Enabled() bool {
	v, ok := u.wf.Lookup(enabledVar)
	if !ok || v == "" {
		return true
	}
	on, err := strconv.ParseBool(v)
	return err != nil || on
}

// Run starts "<binary> update" in the background once a day, and adds a
// pinned "Update available!" item when the last check found a newer version.
// It does nothing without alfred_workflow_version, as in Alfred UI scripts,
// or when check_for_updates is false.
func (u *Updater) Run() error {
	if u.wf.Env.Version == "" || !u.Enabled() {
		return nil
	}
	if u.wf.Cache.Expired(stateKey, interval) {
		if _, err := u.wf.Start(jobName, jobName); err != nil {
			return fmt.Errorf("start update check: %w", err)
		}
	}
	if u.Available() {
		u.wf.NewItem("Update available!").
			Pin().
			Subtitle("Press ⏎ to install").
			Autocomplete("workflow:update").
			Icon(alfred.IconInfo)
	}
	return nil
}

// Check fetches the newest release that has an .alfredworkflow asset and
// saves it for Available. The update command calls this.
func (u *Updater) Check(ctx context.Context) error {
	latest, err := u.fetchLatest(ctx)
	if err != nil {
		// Rewrite the old result so the next check waits a day instead of
		// starting again on every keystroke while offline.
		var old release
		_ = u.wf.Cache.LoadJSONIfExists(stateKey, &old) //nolint:errcheck // a broken file is replaced by an empty result
		if serr := u.wf.Cache.StoreJSON(stateKey, old); serr != nil {
			log.Printf("save update state: %v", serr)
		}
		return err
	}
	// Saved even when empty: the file's age is when the next check is due.
	return u.wf.Cache.StoreJSON(stateKey, latest)
}

func (u *Updater) fetchLatest(ctx context.Context) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.apiURL+"/repos/"+u.repo+"/releases", nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.client.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("fetch releases: %s", resp.Status)
	}

	var releases []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return release{}, fmt.Errorf("decode releases: %w", err)
	}

	var latest release
	for _, r := range releases {
		if r.Draft || r.Prerelease || (latest.Version != "" && !newer(r.Tag, latest.Version)) {
			continue
		}
		for _, a := range r.Assets {
			if strings.HasSuffix(a.Name, ".alfredworkflow") {
				latest = release{Version: r.Tag, URL: a.URL}
				break
			}
		}
	}
	return latest, nil
}

// Available reports whether the last check found a version newer than this one.
func (u *Updater) Available() bool {
	_, ok := u.latest()
	return ok
}

func (u *Updater) latest() (release, bool) {
	var r release
	if err := u.wf.Cache.LoadJSON(stateKey, &r); err != nil {
		return release{}, false
	}
	return r, r.URL != "" && newer(r.Version, u.wf.Env.Version)
}

// Install downloads the newer .alfredworkflow and opens it, which asks Alfred
// to import it.
func (u *Updater) Install(ctx context.Context) error {
	r, ok := u.latest()
	if !ok {
		return errors.New("no update available")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download update: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}

	key := path.Join(path.Dir(stateKey), path.Base(r.URL))
	if err := u.wf.Cache.Store(key, data); err != nil {
		return err
	}
	if out, err := exec.Command("/usr/bin/open", u.wf.Cache.Path(key)).CombinedOutput(); err != nil {
		return fmt.Errorf("open update: %w: %s", err, out)
	}
	log.Printf("installing %s", r.Version)
	return nil
}

// newer reports whether version a is higher than b. Both are semver, with or
// without a leading "v". A pre-release sorts before its release.
func newer(a, b string) bool {
	va, pa := parseVersion(a)
	vb, pb := parseVersion(b)
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	switch {
	case pa == pb:
		return false
	case pa == "":
		return true
	case pb == "":
		return false
	default:
		return pa > pb
	}
}

func parseVersion(s string) ([3]int, string) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	s, pre, _ := strings.Cut(s, "-")
	var v [3]int
	for i, part := range strings.SplitN(s, ".", 3) {
		v[i], _ = strconv.Atoi(part) //nolint:errcheck // a malformed part counts as 0
	}
	return v, pre
}
