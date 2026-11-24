// Package httpcache keeps HTTP GET responses in a workflow's cache, so a
// Script Filter that runs on every keystroke doesn't call the API every time.
//
// Transport is an http.RoundTripper, so it works with any API client that
// takes an *http.Client:
//
//	client := httpcache.New(wf.Cache, 5*time.Minute)
//	resp, err := client.Get("https://api.example.com/items")
//
// A stored response younger than MaxAge is returned without a request. An
// older one is revalidated with If-None-Match or If-Modified-Since when the
// server sent an ETag or Last-Modified, so an unchanged resource costs a 304
// instead of a full download.
package httpcache

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
)

// Header is set on responses the Transport returns from its store, to one of
// the Status values. A response from the server has no Header.
const Header = "X-Alfred-Httpcache"

// Status values of Header.
const (
	// Fresh: younger than MaxAge, returned without a request.
	Fresh = "fresh"
	// Revalidated: the server answered 304 Not Modified.
	Revalidated = "revalidated"
	// Stale: the server failed and StaleIfError is set.
	Stale = "stale"
)

// DefaultDir is the folder in the store that Transport uses when Dir is
// empty.
const DefaultDir = "httpcache"

// Transport stores successful GET responses in Store and answers from it.
// Requests other than GET, Range requests and requests with their own
// conditional headers go straight to Base.
//
// Stored responses are keyed by URL and the Authorization and Accept
// headers, so different accounts never share an entry. The key is a hash;
// credentials are not written, but response bodies are, with the store's
// owner-only permissions.
type Transport struct {
	// Store holds the responses, normally wf.Cache.
	Store *alfred.Store
	// Dir is the folder in Store for the responses. Empty means DefaultDir.
	Dir string
	// MaxAge is how long a stored response is used without asking the
	// server. Zero revalidates every time.
	MaxAge time.Duration
	// StaleIfError returns a stored response of any age when the request
	// fails or the server answers 5xx.
	StaleIfError bool
	// Base makes the requests. Nil means http.DefaultTransport.
	Base http.RoundTripper
}

// New returns a client whose Transport stores responses in store for
// maxAge.
func New(store *alfred.Store, maxAge time.Duration) *http.Client {
	return &http.Client{Transport: &Transport{Store: store, MaxAge: maxAge}}
}

// Status returns resp's Header value: "" for a response from the server.
func Status(resp *http.Response) string { return resp.Header.Get(Header) }

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !cacheable(req) {
		return t.base().RoundTrip(req)
	}
	key := t.key(req)
	cached, age := t.load(key, req)
	if cached != nil && age <= t.MaxAge && !hasToken(req.Header, "Cache-Control", "no-cache") {
		return mark(cached, Fresh), nil
	}

	out := req
	if cached != nil {
		out = req.Clone(req.Context())
		if etag := cached.Header.Get("ETag"); etag != "" {
			out.Header.Set("If-None-Match", etag)
		}
		if lm := cached.Header.Get("Last-Modified"); lm != "" {
			out.Header.Set("If-Modified-Since", lm)
		}
	}
	resp, err := t.base().RoundTrip(out)
	switch {
	case err != nil:
		if cached != nil && t.StaleIfError {
			return mark(cached, Stale), nil
		}
		closeBody(cached)
		return nil, err
	case cached != nil && resp.StatusCode == http.StatusNotModified:
		closeBody(resp)
		now := time.Now()
		if err := os.Chtimes(t.Store.Path(key), now, now); err != nil {
			log.Printf("httpcache: %v", err)
		}
		return mark(cached, Revalidated), nil
	case cached != nil && resp.StatusCode >= 500 && t.StaleIfError:
		closeBody(resp)
		return mark(cached, Stale), nil
	}
	closeBody(cached)

	if resp.StatusCode == http.StatusOK && !hasToken(resp.Header, "Cache-Control", "no-store") {
		// DumpResponse reads the body and gives resp a copy to read again.
		data, err := httputil.DumpResponse(resp, true)
		if err != nil {
			closeBody(resp)
			return nil, err
		}
		if err := t.Store.Store(key, data); err != nil {
			log.Printf("httpcache: %v", err)
		}
	}
	return resp, nil
}

// Clear removes every stored response.
func (t *Transport) Clear() error { return os.RemoveAll(t.Store.Path(t.dir())) }

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *Transport) dir() string {
	if t.Dir != "" {
		return t.Dir
	}
	return DefaultDir
}

func (t *Transport) key(req *http.Request) string {
	h := sha256.New()
	for _, s := range []string{req.URL.String(), req.Header.Get("Authorization"), req.Header.Get("Accept")} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return path.Join(t.dir(), hex.EncodeToString(h.Sum(nil)))
}

// load returns the stored response for key and its age, or nil when there
// is none or it can't be read.
func (t *Transport) load(key string, req *http.Request) (*http.Response, time.Duration) {
	data, err := t.Store.Load(key)
	if err != nil {
		return nil, 0
	}
	age, err := t.Store.Age(key)
	if err != nil {
		return nil, 0
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), req)
	if err != nil {
		log.Printf("httpcache: unreadable entry %s: %v", key, err)
		return nil, 0
	}
	return resp, age
}

func cacheable(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != "" {
		return false
	}
	for _, h := range []string{"Range", "If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since"} {
		if req.Header.Get(h) != "" {
			return false
		}
	}
	return !hasToken(req.Header, "Cache-Control", "no-store")
}

// hasToken reports whether a comma-separated header lists token.
func hasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func mark(resp *http.Response, status string) *http.Response {
	resp.Header.Set(Header, status)
	return resp
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining so the connection can be reused
		_ = resp.Body.Close()                 //nolint:errcheck // nothing left to read
	}
}
