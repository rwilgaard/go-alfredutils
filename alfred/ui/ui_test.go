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

func TestResizeWindow(t *testing.T) {
	tests := []struct {
		name          string
		width, height any
		want          string
	}{
		{"height only", nil, 292, `{"window":{"resize":{"height":292}}}`},
		{"auto width", Auto, 400, `{"window":{"resize":{"width":"auto","height":400}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(Actions{}.ResizeWindow(tt.width, tt.height))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}
