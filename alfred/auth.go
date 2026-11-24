package alfred

import "errors"

// RequireAuth reports whether the Keychain has an entry for account. When it
// doesn't, it sends a "You're not logged in" item with the arg "auth"; when
// the Keychain fails, it sends the error.
func (wf *Workflow) RequireAuth(account string) bool {
	_, err := wf.Keychain.Get(account)
	switch {
	case err == nil:
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

// AddClearAuth registers the "workflow:clearauth" magic query, which deletes
// account from the Keychain. A missing entry counts as cleared.
func (wf *Workflow) AddClearAuth(account string) {
	wf.AddMagic("clearauth", "Clear credentials", "Credentials cleared!", func() error {
		if err := wf.Keychain.Delete(account); !errors.Is(err, ErrNotFound) {
			return err
		}
		return nil
	})
}
