package alfred_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
	"github.com/rwilgaard/go-alfredutils/v2/alfred/alfredtest"
)

type login struct{ id, title, url, vault string }

var logins = []login{
	{"id-github", "GitHub", "https://github.com", "Personal"},
	{"id-ghe", "GitHub Enterprise", "https://ghe.company.com", "Work"},
	{"id-resources", "Developer Resources", "https://github.com/resources", "Personal"},
	{"id-special-aws", "Some Cloud Service", "https://example.com", "Personal"},
	{"id-aws", "AWS Management Console", "https://aws.amazon.com", "Work"},
}

func addLogins(wf *alfred.Workflow) {
	for _, l := range logins {
		wf.NewItem(l.title).
			UID(l.id).
			MatchOn(alfred.MatchSecondary, l.url).
			MatchOn(alfred.MatchMeta, l.vault).
			MatchExact(l.id)
	}
}

func filtered(t *testing.T, wf *alfred.Workflow, query string) []string {
	t.Helper()
	wf.Filter(query)
	var titles []string
	for _, it := range wf.Items() {
		b, err := it.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var v struct{ Title string }
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		titles = append(titles, v.Title)
	}
	return titles
}

func TestFilterRanking(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		noFuzzy bool
		want    []string
	}{
		{name: "title beats url", query: "github", want: []string{"GitHub", "GitHub Enterprise", "Developer Resources"}},
		{name: "without fuzzy", query: "github", noFuzzy: true, want: []string{"GitHub", "GitHub Enterprise", "Developer Resources"}},
		{name: "exact value finds the item", query: "ID-SPECIAL-AWS", want: []string{"Some Cloud Service"}},
		{name: "partial exact value does not match", query: "special", want: nil},
		{name: "every word must match a field", query: "github work", want: []string{"GitHub Enterprise"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf, _ := alfredtest.New(t, nil)
			wf.FuzzyMatch(!tt.noFuzzy)
			addLogins(wf)
			if got := filtered(t, wf, tt.query); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterUsage(t *testing.T) {
	record := func(t *testing.T, wf *alfred.Workflow, id string, n int) {
		t.Helper()
		for range n {
			if err := wf.Usage.Record(id); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("empty query orders by usage, unused keep their order", func(t *testing.T) {
		wf, _ := alfredtest.New(t, nil, alfred.WithUsage())
		record(t, wf, "id-aws", 3)
		record(t, wf, "id-resources", 1)
		addLogins(wf)
		want := []string{"AWS Management Console", "Developer Resources", "GitHub", "GitHub Enterprise", "Some Cloud Service"}
		if got := filtered(t, wf, ""); !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("usage reorders within a match tier", func(t *testing.T) {
		wf, _ := alfredtest.New(t, nil, alfred.WithUsage())
		wf.FuzzyMatch(false)
		record(t, wf, "id-ghe", 20)
		addLogins(wf)
		if got := filtered(t, wf, "githu"); got[0] != "GitHub Enterprise" {
			t.Errorf("got %q, want GitHub Enterprise first", got)
		}
	})

	t.Run("usage does not beat a stronger match", func(t *testing.T) {
		wf, _ := alfredtest.New(t, nil, alfred.WithUsage())
		record(t, wf, "id-resources", 50)
		addLogins(wf)
		if got := filtered(t, wf, "github"); got[0] != "GitHub" {
			t.Errorf("got %q, want GitHub first", got)
		}
	})

	t.Run("recent", func(t *testing.T) {
		wf, _ := alfredtest.New(t, nil, alfred.WithUsage())
		if _, ok := wf.Usage.Recent(time.Minute); ok {
			t.Error("empty store reported a recent item")
		}
		now := time.Now()
		alfred.SetUsageClock(wf.Usage, func() time.Time { return now })
		record(t, wf, "a", 1)
		now = now.Add(time.Second)
		record(t, wf, "b", 1)
		if id, ok := wf.Usage.Recent(time.Minute); !ok || id != "b" {
			t.Errorf("Recent() = %q, %v, want b", id, ok)
		}
		if _, ok := wf.Usage.Recent(0); ok {
			t.Error("Recent(0) reported an item")
		}
	})
}

func TestPin(t *testing.T) {
	t.Run("survives filtering and stays first", func(t *testing.T) {
		wf, _ := alfredtest.New(t, nil, alfred.WithUsage())
		if err := wf.Usage.Record("id-github"); err != nil {
			t.Fatal(err)
		}
		addLogins(wf)
		wf.NewItem("Update available!").Pin()
		want := []string{"Update available!", "GitHub", "GitHub Enterprise", "Developer Resources"}
		if got := filtered(t, wf, "github"); !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("send puts pinned first and adds no-results when nothing else", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		wf.NewItem("a")
		wf.NewItem("pinned").Pin()
		wf.Filter("zzz")
		wf.NoResults("Nothing", "")
		wf.Send()
		want := []string{"pinned", "Nothing"}
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("send puts pinned first without filter", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		wf.NewItem("a")
		wf.NewItem("pinned").Pin()
		wf.Send()
		want := []string{"pinned", "a"}
		if got := alfredtest.Decode(t, out).Titles(); !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestUsageOutput(t *testing.T) {
	t.Run("prefixes uids and skips knowledge", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil, alfred.WithUsage())
		wf.NewItem("a").UID("id-a")
		wf.NewItem("b")
		wf.Send()
		fb := alfredtest.Decode(t, out)
		if !fb.SkipKnowledge {
			t.Error("skipknowledge not set")
		}
		if fb.Items[0].UID != "u:id-a" || fb.Items[1].UID != "" {
			t.Errorf("uids = %q, %q", fb.Items[0].UID, fb.Items[1].UID)
		}
	})

	t.Run("leaves uids alone without usage", func(t *testing.T) {
		wf, out := alfredtest.New(t, nil)
		wf.NewItem("a").UID("id-a")
		wf.Send()
		fb := alfredtest.Decode(t, out)
		if fb.SkipKnowledge || fb.Items[0].UID != "id-a" {
			t.Errorf("got skipknowledge=%v uid=%q", fb.SkipKnowledge, fb.Items[0].UID)
		}
	})
}
