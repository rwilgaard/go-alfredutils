package alfred

import "testing"

func TestParsePassword(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    string
		wantErr bool
	}{
		{name: "ascii", out: `password: "s3cret"`, want: "s3cret"},
		{name: "ascii with quotes inside", out: `password: "a"b"`, want: `a"b`},
		{name: "non-ascii is hex", out: `password: 0x74C3AB7374  "t\303\253st"`, want: "tëst"},
		{name: "no password line", out: `keychain: "login"`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePassword(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
