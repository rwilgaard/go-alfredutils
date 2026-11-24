package totp_test

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred/totp"
)

// RFC 6238 appendix B test vectors (8 digits). The seeds are the RFC's
// ASCII secrets, base32-encoded as password managers store them.
func TestCodeRFC6238(t *testing.T) {
	seed := func(n int) string {
		return base32.StdEncoding.WithPadding(base32.NoPadding).
			EncodeToString([]byte(strings.Repeat("1234567890", 7)[:n]))
	}
	sha1Key, sha256Key, sha512Key := seed(20), seed(32), seed(64)
	tests := []struct {
		name, uri string
		unix      int64
		want      string
	}{
		{"sha1 59", "otpauth://totp/x?digits=8&secret=" + sha1Key, 59, "94287082"},
		{"sha1 1111111109", "otpauth://totp/x?digits=8&secret=" + sha1Key, 1111111109, "07081804"},
		{"sha256 59", "otpauth://totp/x?digits=8&algorithm=SHA256&secret=" + sha256Key, 59, "46119246"},
		{"sha512 59", "otpauth://totp/x?digits=8&algorithm=SHA512&secret=" + sha512Key, 59, "90693936"},
		{"sha1 2000000000", "otpauth://totp/x?digits=8&secret=" + sha1Key, 2000000000, "69279037"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := totp.Parse(tt.uri)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := k.Code(time.Unix(tt.unix, 0)); got != tt.want {
				t.Errorf("code = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseFormats(t *testing.T) {
	tests := []struct {
		name, in   string
		wantDigits int
		wantSteam  bool
		wantErr    bool
	}{
		{"bare base32 with spaces and lowercase", "gezd gnbv gy3t qojq", 6, false, false},
		{"steam", "steam://GEZDGNBVGY3TQOJQ", 5, true, false},
		{"otpauth defaults", "otpauth://totp/Issuer:me?secret=GEZDGNBVGY3TQOJQ&issuer=Issuer", 6, false, false},
		{"empty", "  ", 0, false, true},
		{"bad base32", "not-base32!", 0, false, true},
		{"bad algorithm", "otpauth://totp/x?secret=GEZDGNBV&algorithm=MD5", 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := totp.Parse(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if k.Digits != tt.wantDigits || k.Steam != tt.wantSteam {
				t.Errorf("digits=%d steam=%v", k.Digits, k.Steam)
			}
			code, rem := k.Code(time.Unix(59, 0))
			if len(code) != tt.wantDigits || rem != k.Period-59%k.Period {
				t.Errorf("code=%q remaining=%d", code, rem)
			}
		})
	}
}

func TestGroup(t *testing.T) {
	for in, want := range map[string]string{
		"123456":   "123 456",
		"12345678": "1234 5678",
		"VK2TQ":    "VK2TQ",
		"1234567":  "1234567",
		"":         "",
	} {
		if got := totp.Group(in); got != want {
			t.Errorf("Group(%q) = %q, want %q", in, got, want)
		}
	}
}
