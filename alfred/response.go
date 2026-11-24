package alfred

import (
	"encoding/json"
	"maps"
	"sort"
	"strings"
	"time"

	"go.deanishe.net/fuzzy"
)

// Response is the output of a Script Filter.
type Response struct {
	items         []*Item
	rerun         time.Duration
	vars          map[string]string
	cache         *cacheConfig
	skipKnowledge bool
	fuzzyOpts     []fuzzy.Option
}

type cacheConfig struct {
	Seconds     int  `json:"seconds"`
	LooseReload bool `json:"loosereload,omitempty"`
}

// NewResponse returns an empty Response.
func NewResponse() *Response { return &Response{} }

// NewItem adds an item. Like awgo, items are not actionable until Valid(true).
func (r *Response) NewItem(title string) *Item {
	it := &Item{title: title}
	r.items = append(r.items, it)
	return it
}

// Items returns the items in their current order.
func (r *Response) Items() []*Item { return r.items }

// IsEmpty reports whether the response has no items.
func (r *Response) IsEmpty() bool { return len(r.items) == 0 }

// Rerun asks Alfred to run the Script Filter again after d, which Alfred
// limits to 0.1–5 s.
func (r *Response) Rerun(d time.Duration) *Response {
	r.rerun = min(max(d, 100*time.Millisecond), 5*time.Second)
	return r
}

// Var sets a variable on every item.
func (r *Response) Var(name, value string) *Response {
	if r.vars == nil {
		r.vars = map[string]string{}
	}
	r.vars[name] = value
	return r
}

// Cache lets Alfred reuse this response for d (5 s to 24 h) instead of
// running the Script Filter. With looseReload, Alfred shows the cached
// results while it runs the script again. Requires Alfred 5.
func (r *Response) Cache(d time.Duration, looseReload bool) *Response {
	secs := int(min(max(d, 5*time.Second), 24*time.Hour) / time.Second)
	r.cache = &cacheConfig{Seconds: secs, LooseReload: looseReload}
	return r
}

// SkipKnowledge stops Alfred learning from the selected item. Requires Alfred 5.
func (r *Response) SkipKnowledge() *Response {
	r.skipKnowledge = true
	return r
}

// Filter keeps the items that fuzzy-match query, best match first. It matches
// an item's Match text, or its title when Match isn't set. An empty query
// leaves the items as they are.
func (r *Response) Filter(query string) {
	if query == "" {
		return
	}
	results := fuzzy.New(itemList(r.items), r.fuzzyOpts...).Sort(query)
	kept := r.items[:0]
	for i, res := range results {
		if res.Match {
			kept = append(kept, r.items[i])
		}
	}
	clear(r.items[len(kept):])
	r.items = kept
}

type itemList []*Item

func (l itemList) Len() int           { return len(l) }
func (l itemList) Less(int, int) bool { return false } // equal scores keep no particular order, as in awgo
func (l itemList) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }
func (l itemList) Keywords(i int) string {
	if l[i].match != nil {
		return *l[i].match
	}
	return l[i].title
}

func (r *Response) MarshalJSON() ([]byte, error) {
	items := r.items
	if items == nil {
		items = []*Item{}
	}
	return json.Marshal(struct {
		Items         []*Item           `json:"items"`
		Rerun         float64           `json:"rerun,omitempty"`
		Variables     map[string]string `json:"variables,omitempty"`
		Cache         *cacheConfig      `json:"cache,omitempty"`
		SkipKnowledge bool              `json:"skipknowledge,omitempty"`
	}{items, r.rerun.Seconds(), r.vars, r.cache, r.skipKnowledge})
}

// Icon is an item or modifier icon.
type Icon struct {
	Path string `json:"path"`
	// Type is "" for an image file, "fileicon" for the Finder icon of the file
	// at Path, or "filetype" for the icon of the UTI in Path.
	Type string `json:"type,omitempty"`
}

const coreTypes = "/System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/"

// Icons from the workflow and from macOS.
var (
	IconWorkflow = Icon{Path: "icon.png"}
	IconInfo     = Icon{Path: coreTypes + "ToolbarInfo.icns"}
	IconError    = Icon{Path: coreTypes + "AlertStopIcon.icns"}
	IconWarning  = Icon{Path: coreTypes + "AlertCautionIcon.icns"}
	IconHome     = Icon{Path: coreTypes + "HomeFolderIcon.icns"}
)

// ItemAction is what Universal Actions act on for an item. Requires Alfred 5.
type ItemAction struct {
	Text []string `json:"text,omitempty"`
	URL  string   `json:"url,omitempty"`
	File string   `json:"file,omitempty"`
	Auto string   `json:"auto,omitempty"`
}

// Item is one Script Filter result.
type Item struct {
	title        string
	subtitle     string
	uid          string
	arg          []string
	argSet       bool
	valid        bool
	autocomplete string
	match        *string
	typ          string
	icon         *Icon
	copytext     string
	largetype    string
	quicklook    string
	vars         map[string]string
	mods         map[string]*Modifier
	action       *ItemAction
}

func (it *Item) Subtitle(s string) *Item { it.subtitle = s; return it }

// Arg is what the next object receives. More than one value is sent as an array.
func (it *Item) Arg(args ...string) *Item { it.arg, it.argSet = args, true; return it }

// UID lets Alfred learn which items get picked and rank them first.
func (it *Item) UID(uid string) *Item { it.uid = uid; return it }

// Valid makes the item actionable with ⏎.
func (it *Item) Valid(v bool) *Item { it.valid = v; return it }

// Autocomplete replaces the query when the item is tab-completed, or actioned
// while invalid.
func (it *Item) Autocomplete(s string) *Item { it.autocomplete = s; return it }

// Match is the text Filter and Alfred's own filtering match on, instead of the title.
func (it *Item) Match(s string) *Item { it.match = &s; return it }

func (it *Item) Icon(icon Icon) *Item { it.icon = &icon; return it }

// Copytext is copied by ⌘C instead of the arg.
func (it *Item) Copytext(s string) *Item { it.copytext = s; return it }

// Largetype is shown by ⌘L instead of the title.
func (it *Item) Largetype(s string) *Item { it.largetype = s; return it }

func (it *Item) Quicklook(url string) *Item { it.quicklook = url; return it }

// Var sets a variable passed on when the item is actioned. Modifiers created
// after this call get it too.
func (it *Item) Var(name, value string) *Item {
	if it.vars == nil {
		it.vars = map[string]string{}
	}
	it.vars[name] = value
	return it
}

// File marks the item as a file at path, so Alfred offers file actions on it.
// With skipCheck, Alfred doesn't check that the file exists.
func (it *Item) File(path string, skipCheck bool) *Item {
	it.typ = "file"
	if skipCheck {
		it.typ = "file:skipcheck"
	}
	if !it.argSet {
		it.Arg(path)
	}
	return it
}

// Action sets what Universal Actions act on. Requires Alfred 5.
func (it *Item) Action(a ItemAction) *Item { it.action = &a; return it }

// NewModifier adds the result shown while keys are held. It starts with a
// copy of the item's variables, like awgo's.
func (it *Item) NewModifier(keys ...Mod) *Modifier {
	m := &Modifier{vars: maps.Clone(it.vars)}
	if it.mods == nil {
		it.mods = map[string]*Modifier{}
	}
	it.mods[modKey(keys)] = m
	return m
}

func (it *Item) MarshalJSON() ([]byte, error) {
	var text map[string]string
	if it.copytext != "" || it.largetype != "" {
		text = map[string]string{}
		if it.copytext != "" {
			text["copy"] = it.copytext
		}
		if it.largetype != "" {
			text["largetype"] = it.largetype
		}
	}
	return json.Marshal(struct {
		UID          string               `json:"uid,omitempty"`
		Title        string               `json:"title"`
		Subtitle     string               `json:"subtitle,omitempty"`
		Arg          any                  `json:"arg,omitempty"`
		Valid        bool                 `json:"valid"`
		Autocomplete string               `json:"autocomplete,omitempty"`
		Match        *string              `json:"match,omitempty"`
		Type         string               `json:"type,omitempty"`
		Icon         *Icon                `json:"icon,omitempty"`
		Text         map[string]string    `json:"text,omitempty"`
		Quicklook    string               `json:"quicklookurl,omitempty"`
		Variables    map[string]string    `json:"variables,omitempty"`
		Mods         map[string]*Modifier `json:"mods,omitempty"`
		Action       *ItemAction          `json:"action,omitempty"`
	}{
		UID:          it.uid,
		Title:        it.title,
		Subtitle:     it.subtitle,
		Arg:          argValue(it.arg, it.argSet),
		Valid:        it.valid,
		Autocomplete: it.autocomplete,
		Match:        it.match,
		Type:         it.typ,
		Icon:         it.icon,
		Text:         text,
		Quicklook:    it.quicklook,
		Variables:    it.vars,
		Mods:         it.mods,
		Action:       it.action,
	})
}

// argValue keeps an explicitly empty arg, which Alfred treats differently
// from none: it puts "" in the search field instead of the item's title.
func argValue(args []string, set bool) any {
	switch {
	case !set:
		return nil
	case len(args) == 0:
		return ""
	case len(args) == 1:
		return args[0]
	default:
		return args
	}
}

// Mod is a modifier key.
type Mod string

const (
	ModCmd   Mod = "cmd"
	ModOpt   Mod = "alt"
	ModCtrl  Mod = "ctrl"
	ModShift Mod = "shift"
	ModFn    Mod = "fn"
)

func modKey(keys []Mod) string {
	s := make([]string, len(keys))
	for i, k := range keys {
		s[i] = string(k)
	}
	sort.Strings(s)
	return strings.Join(s, "+")
}

// Modifier is the result shown while modifier keys are held.
type Modifier struct {
	subtitle *string
	arg      []string
	argSet   bool
	valid    bool
	icon     *Icon
	vars     map[string]string
}

func (m *Modifier) Subtitle(s string) *Modifier { m.subtitle = &s; return m }
func (m *Modifier) Arg(args ...string) *Modifier {
	m.arg, m.argSet = args, true
	return m
}
func (m *Modifier) Valid(v bool) *Modifier { m.valid = v; return m }

// Icon replaces the item's icon while the keys are held. Requires Alfred 5.
func (m *Modifier) Icon(icon Icon) *Modifier { m.icon = &icon; return m }

func (m *Modifier) Var(name, value string) *Modifier {
	if m.vars == nil {
		m.vars = map[string]string{}
	}
	m.vars[name] = value
	return m
}

func (m *Modifier) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Subtitle  *string           `json:"subtitle,omitempty"`
		Arg       any               `json:"arg,omitempty"`
		Valid     bool              `json:"valid,omitempty"`
		Icon      *Icon             `json:"icon,omitempty"`
		Variables map[string]string `json:"variables,omitempty"`
	}{m.subtitle, argValue(m.arg, m.argSet), m.valid, m.icon, m.vars})
}
