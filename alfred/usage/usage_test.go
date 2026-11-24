package usage

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestRecordDecay(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name  string
		uses  []time.Duration // offsets from t0
		at    time.Duration
		score float64
	}{
		{"single use, now", []time.Duration{0}, 0, 1},
		{"single use, one half-life later", []time.Duration{0}, HalfLife, 0.5},
		{"two uses, one half-life apart", []time.Duration{0, HalfLife}, HalfLife, 1.5},
		{"two uses at once, two half-lives later", []time.Duration{0, 0}, 2 * HalfLife, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Store{}
			for _, u := range tt.uses {
				s.Record("a", t0.Add(u))
			}
			if got := s.Scores(t0.Add(tt.at))["a"]; math.Abs(got-tt.score) > 1e-9 {
				t.Errorf("score = %v, want %v", got, tt.score)
			}
		})
	}
}

func TestRecordPrunes(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	s := Store{}
	s.Record("old", t0)
	s.Record("fresh", t0.Add(10*HalfLife))
	if _, ok := s["old"]; ok {
		t.Error("faded entry was not pruned")
	}
	if _, ok := s["fresh"]; !ok {
		t.Error("recorded entry missing")
	}
}

func TestLegacySeconds(t *testing.T) {
	var s Store
	if err := json.Unmarshal([]byte(`{"a":{"s":2,"t":1700000000}}`), &s); err != nil {
		t.Fatal(err)
	}
	t0 := time.Unix(1_700_000_000, 0)
	if got := s.Scores(t0.Add(HalfLife))["a"]; math.Abs(got-1) > 1e-9 {
		t.Errorf("score = %v, want 1", got)
	}
	if _, at, _ := s.Latest(); !at.Equal(t0) {
		t.Errorf("Latest() at %v, want %v", at, t0)
	}
}

func TestLatestWithinASecond(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	s := Store{}
	s.Record("a", t0)
	s.Record("b", t0.Add(10*time.Millisecond))
	if id, _, _ := s.Latest(); id != "b" {
		t.Errorf("Latest() = %q, want b", id)
	}
}

func TestLatest(t *testing.T) {
	if _, _, ok := (Store{}).Latest(); ok {
		t.Error("empty store reported a latest item")
	}
	t0 := time.Unix(1_700_000_000, 0)
	s := Store{}
	s.Record("a", t0)
	s.Record("b", t0.Add(time.Minute))
	s.Record("a", t0.Add(-time.Minute)) // out-of-order clock still records
	id, at, ok := s.Latest()
	if !ok || id != "b" || !at.Equal(t0.Add(time.Minute)) {
		t.Errorf("Latest() = %q, %v, %v", id, at, ok)
	}
}
