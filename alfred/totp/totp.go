// Package totp generates time-based one-time passwords (RFC 6238) from the
// secret formats password managers store: otpauth:// URIs, steam:// secrets
// and bare base32 keys.
package totp

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // SHA-1 is the RFC 6238 default and what most issuers use.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const steamAlphabet = "23456789BCDFGHJKMNPQRTVWXY"

// Key is a parsed TOTP secret.
type Key struct {
	Secret    []byte
	Digits    int
	Period    int
	Algorithm func() hash.Hash
	Steam     bool
}

// Parse parses a TOTP secret: an otpauth:// URI, a steam:// secret, or a
// bare base32 key (spaces, dashes and padding are ignored).
func Parse(s string) (*Key, error) {
	s = strings.TrimSpace(s)
	k := &Key{Digits: 6, Period: 30, Algorithm: sha1.New}

	var secret string
	switch {
	case strings.HasPrefix(strings.ToLower(s), "otpauth://"):
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("parse otpauth uri: %w", err)
		}
		q := u.Query()
		secret = q.Get("secret")
		if v := q.Get("digits"); v != "" {
			if k.Digits, err = strconv.Atoi(v); err != nil || k.Digits < 1 || k.Digits > 10 {
				return nil, fmt.Errorf("invalid digits %q", v)
			}
		}
		if v := q.Get("period"); v != "" {
			if k.Period, err = strconv.Atoi(v); err != nil || k.Period < 1 {
				return nil, fmt.Errorf("invalid period %q", v)
			}
		}
		switch strings.ToUpper(q.Get("algorithm")) {
		case "", "SHA1":
		case "SHA256":
			k.Algorithm = sha256.New
		case "SHA512":
			k.Algorithm = sha512.New
		default:
			return nil, fmt.Errorf("unsupported algorithm %q", q.Get("algorithm"))
		}
	case strings.HasPrefix(strings.ToLower(s), "steam://"):
		secret = s[len("steam://"):]
		k.Steam, k.Digits = true, 5
	default:
		secret = s
	}

	secret = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(secret))
	if secret == "" {
		return nil, errors.New("empty totp secret")
	}
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("decode totp secret: %w", err)
	}
	k.Secret = b
	return k, nil
}

// Current returns the code for secret at the current time.
func Current(secret string) (string, error) {
	k, err := Parse(secret)
	if err != nil {
		return "", err
	}
	code, _ := k.Code(time.Now())
	return code, nil
}

// Code returns the code at t and the number of seconds it stays valid.
func (k *Key) Code(t time.Time) (code string, remaining int) {
	unix := t.Unix()
	counter := uint64(unix / int64(k.Period)) //nolint:gosec // unix time is positive.
	remaining = k.Period - int(unix%int64(k.Period))

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(k.Algorithm, k.Secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	if k.Steam {
		out := make([]byte, k.Digits)
		for i := range out {
			out[i] = steamAlphabet[bin%uint32(len(steamAlphabet))]
			bin /= uint32(len(steamAlphabet))
		}
		return string(out), remaining
	}

	mod := uint32(1)
	for range k.Digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", k.Digits, bin%mod), remaining
}

// Group splits an even-length code of 6 or more digits in half for
// readability: "123456" becomes "123 456". Other codes are returned as is.
func Group(code string) string {
	if n := len(code); n >= 6 && n%2 == 0 {
		return code[:n/2] + " " + code[n/2:]
	}
	return code
}
