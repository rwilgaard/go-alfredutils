package plist

import (
	"reflect"
	"testing"
)

func TestDecode(t *testing.T) {
	got, err := Decode([]byte(`<?xml version="1.0"?><plist version="1.0"><dict>
		<key>s</key><string>a &amp; b</string>
		<key>n</key><integer>3</integer>
		<key>t</key><true/>
		<key>empty</key><string></string>
		<key>a</key><array><string>x</string><dict><key>k</key><false/></dict></array>
	</dict></plist>`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"s": "a & b", "n": "3", "t": true, "empty": "",
		"a": []any{"x", map[string]any{"k": false}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Decode = %#v, want %#v", got, want)
	}
}
