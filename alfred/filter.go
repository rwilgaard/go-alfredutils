package alfred

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"go.deanishe.net/fuzzy"
)

// MatchWeight is how much a match on one of an item's fields counts in Filter.
type MatchWeight int

const (
	// MatchPrimary is for the field users search by, usually the name. Without
	// a MatchOn(MatchPrimary, …) call, an item's Match text or title is used.
	MatchPrimary MatchWeight = iota
	// MatchSecondary is for supporting text such as a URL or path.
	MatchSecondary
	// MatchMeta is for grouping text such as a folder, vault or category.
	MatchMeta
)

// weights are the scores for an exact, prefix, contains and fuzzy match on a field.
type weights struct{ exact, prefix, contains, fuzzy float64 }

// The tiers are far enough apart that a stronger kind of match on a field
// always beats a weaker one, whatever the fuzzy score or usage boost adds.
var (
	fieldWeights = [...]weights{
		MatchPrimary:   {1000, 500, 200, 100},
		MatchSecondary: {100, 50, 30, 10},
		MatchMeta:      {50, 30, 20, 5},
	}
	// phraseWeights score a multi-word query as one phrase against the primary fields.
	phraseWeights = weights{2000, 1000, 400, 200}
)

// exactBonus is for a word equal to a MatchExact value. Those only match
// exactly: IDs are random, so partial or fuzzy hits on them are noise.
const exactBonus = 800

// usageWeight scales the usage boost. Heavy use (score ~50) adds ~280, enough
// to reorder items within a match tier but not to lift a contains match over a
// prefix match (a 300 gap).
const usageWeight = 50

type matchField struct {
	text   string
	weight MatchWeight
}

// filterConfig is what Filter needs beyond the items. FatalError keeps it
// when it replaces the response.
type filterConfig struct {
	fuzzyOpts []fuzzy.Option
	noFuzzy   bool
	usage     *Usage
}

// FuzzyMatch turns fuzzy matching in Filter on or off. It is on by default;
// off, only exact, prefix and substring matches count.
func (r *Response) FuzzyMatch(on bool) *Response {
	r.filter.noFuzzy = !on
	return r
}

// Filter keeps the items that match query, best first. Every word of the
// query must match at least one of an item's fields (see MatchOn), and an
// exact match beats a prefix match, which beats a substring match, which
// beats a fuzzy one. With WithUsage, often and recently used items move up
// within the same tier. Ties keep the order the items were added in.
//
// An empty query keeps every item, ordered by usage with WithUsage and left
// as it is without. Pinned items are never filtered or reordered.
func (r *Response) Filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	var scores map[string]float64
	if r.filter.usage != nil {
		scores = r.filter.usage.Scores()
	}
	boost := func(it *Item) float64 {
		if it.uid == "" {
			return 0
		}
		return usageWeight * math.Log2(1+scores[it.uid])
	}

	pinned, rest := partitionPinned(r.items)
	if query == "" {
		if len(scores) > 0 {
			slices.SortStableFunc(rest, func(a, b *Item) int { return cmp.Compare(boost(b), boost(a)) })
		}
		r.items = append(pinned, rest...)
		return
	}

	type scored struct {
		item  *Item
		score float64
	}
	words := strings.Fields(query)
	var matches []scored
	for _, it := range rest {
		if s, ok := r.scoreItem(it, query, words); ok {
			matches = append(matches, scored{it, s + boost(it)})
		}
	}
	slices.SortStableFunc(matches, func(a, b scored) int { return cmp.Compare(b.score, a.score) })

	items := pinned
	for _, m := range matches {
		items = append(items, m.item)
	}
	r.items = items
}

// partitionPinned splits items into pinned and the rest, keeping their order.
func partitionPinned(items []*Item) (pinned, rest []*Item) {
	for _, it := range items {
		if it.pinned {
			pinned = append(pinned, it)
		} else {
			rest = append(rest, it)
		}
	}
	return pinned, rest
}

// fields returns what Filter matches an item on. Without an explicit primary
// field, the Match text or title is the primary one.
func (it *Item) fields() []matchField {
	if slices.ContainsFunc(it.matchFields, func(f matchField) bool { return f.weight == MatchPrimary }) {
		return it.matchFields
	}
	primary := it.title
	if it.match != nil {
		primary = *it.match
	}
	return append([]matchField{{primary, MatchPrimary}}, it.matchFields...)
}

// scoreItem scores an item against a lower-case query split into words.
func (r *Response) scoreItem(it *Item, query string, words []string) (float64, bool) {
	fields := it.fields()
	var total float64
	// A multi-word query also earns a bonus when the whole phrase matches a primary field.
	if len(words) > 1 {
		for _, f := range fields {
			if f.weight == MatchPrimary {
				if s, ok := r.scoreField(f.text, query, phraseWeights); ok {
					total += s
				}
			}
		}
	}
	for _, w := range words {
		s, ok := r.scoreWord(it, fields, w)
		if !ok {
			return 0, false
		}
		total += s
	}
	return total, true
}

// scoreWord scores one lower-case query word against all of an item's fields.
func (r *Response) scoreWord(it *Item, fields []matchField, word string) (float64, bool) {
	var total float64
	var matched bool
	for _, f := range fields {
		if s, ok := r.scoreField(f.text, word, fieldWeights[f.weight]); ok {
			total += s
			matched = true
		}
	}
	for _, e := range it.exact {
		if strings.EqualFold(e, word) {
			total += exactBonus
			matched = true
		}
	}
	return total, matched
}

// scoreField scores val against term, which must already be lower case.
func (r *Response) scoreField(val, term string, w weights) (float64, bool) {
	if val == "" {
		return 0, false
	}
	lower := strings.ToLower(val)
	if lower == term {
		return w.exact, true
	}

	var score float64
	var matched bool
	if strings.HasPrefix(lower, term) {
		score, matched = w.prefix, true
	} else if strings.Contains(lower, term) {
		score, matched = w.contains, true
	}
	if !r.filter.noFuzzy {
		if res := fuzzy.Match(val, term, r.filter.fuzzyOpts...); res.Match {
			score += w.fuzzy + res.Score
			matched = true
		}
	}
	return score, matched
}
