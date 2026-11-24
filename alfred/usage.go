package alfred

import (
	"log"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred/usage"
)

// usageKey is in the data dir, not the cache, so clearing the cache keeps the learned ranking.
const usageKey = "usage.json"

// uidPrefix is put in front of every UID while usage ranking is on. Alfred
// keeps applying knowledge it learned before skipknowledge was set, so new
// UIDs start without any.
const uidPrefix = "u:"

// Usage counts which items get used, so Filter can rank them first. Each use
// adds 1 to an item's score and scores halve every week, so items used often
// and recently rank highest. Items are identified by the id given to UID.
//
// While usage ranking is on, every response sets skipknowledge and its UIDs
// get a fixed prefix, so Alfred never reorders the results: the order is
// match quality, then usage, with pinned items first.
type Usage struct {
	store *Store
	now   func() time.Time
}

// WithUsage turns on usage ranking. Record uses with wf.Usage.Record.
func WithUsage() Option {
	return func(wf *Workflow) { wf.withUsage = true }
}

func (u *Usage) load() usage.Store {
	s := usage.Store{}
	if err := u.store.LoadJSONIfExists(usageKey, &s); err != nil {
		log.Printf("load usage: %v", err)
		return usage.Store{}
	}
	return s
}

// Record counts a use of the item with UID id.
func (u *Usage) Record(id string) error {
	s := u.load()
	s.Record(id, u.now())
	return u.store.StoreJSON(usageKey, s)
}

// Scores returns the current score of every used item by UID.
func (u *Usage) Scores() map[string]float64 { return u.load().Scores(u.now()) }

// Recent returns the most recently used item if it was used within d, for
// a "recently used" row or hotkey.
func (u *Usage) Recent(d time.Duration) (id string, ok bool) {
	id, usedAt, ok := u.load().Latest()
	return id, ok && u.now().Sub(usedAt) < d
}
