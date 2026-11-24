package ui

import (
	"encoding/json"
	"testing"
)

func TestReplaceAndAppend(t *testing.T) {
	a := NewUpdate().
		Set("status", Props{"text": "done"}).
		ReplaceWith("label", Props{"type": "textarea"}).
		Append("main", Props{"type": "label", "text": "a"}).
		Append("main", Props{"type": "label", "text": "b"})
	got, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"insert":{"ids":{"main":[{"text":"a","type":"label"},{"text":"b","type":"label"}]}},` +
		`"update":{"ids":{"status":{"text":"done"}}},"replace":{"ids":{"label":{"type":"textarea"}}}}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
