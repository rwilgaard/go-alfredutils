package alfred

import (
	"errors"
	"log"
	"net/url"
	"path"
)

// Workflows that check for credentials on every keystroke would pay ~20 ms
// for /usr/bin/security each time. A marker file in the data folder, kept in
// step with the Keychain entry, answers "is there a login" instead.
func authMarkerKey(account string) string { return path.Join("_auth", url.PathEscape(account)) }

// SetAuth stores secret for account in the Keychain and marks the account as
// logged in.
func (wf *Workflow) SetAuth(account, secret string) error {
	if err := wf.Keychain.Set(account, secret); err != nil {
		return err
	}
	return wf.Data.Store(authMarkerKey(account), nil)
}

// LoadAuth returns the secret for account. When the Keychain has none, it
// also removes the marker, so the next RequireAuth asks to log in again.
func (wf *Workflow) LoadAuth(account string) (string, error) {
	secret, err := wf.Keychain.Get(account)
	if errors.Is(err, ErrNotFound) {
		_ = wf.Data.Delete(authMarkerKey(account)) //nolint:errcheck // the not-found error is the one to report
	}
	return secret, err
}

// ClearAuth deletes account from the Keychain and removes its marker. A
// missing entry counts as cleared.
func (wf *Workflow) ClearAuth(account string) error {
	if err := wf.Keychain.Delete(account); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return wf.Data.Delete(authMarkerKey(account))
}

// RequireAuth reports whether account is logged in. When it isn't, it sends a
// "You're not logged in" item with the arg "auth"; when the Keychain fails, it
// sends the error.
//
// It reads the marker, not the Keychain, so it's cheap enough for every
// keystroke. Without a marker it checks the Keychain once and writes one, so
// entries stored before SetAuth existed still count.
func (wf *Workflow) RequireAuth(account string) bool {
	if wf.Data.Exists(authMarkerKey(account)) {
		return true
	}
	_, err := wf.Keychain.Get(account)
	switch {
	case err == nil:
		if serr := wf.Data.Store(authMarkerKey(account), nil); serr != nil {
			log.Printf("write auth marker: %v", serr)
		}
		return true
	case errors.Is(err, ErrNotFound):
		wf.NewItem("You're not logged in.").
			Subtitle("Press ⏎ to authenticate").
			Icon(IconInfo).
			Arg("auth").
			Valid(true)
	default:
		wf.NewItem("Keychain error.").
			Subtitle(err.Error()).
			Icon(IconWarning)
	}
	wf.Send()
	return false
}

// AddClearAuth registers the "workflow:clearauth" magic query, which runs
// ClearAuth for account.
func (wf *Workflow) AddClearAuth(account string) {
	wf.AddMagic("clearauth", "Clear credentials", "Credentials cleared!", func() error {
		return wf.ClearAuth(account)
	})
}
