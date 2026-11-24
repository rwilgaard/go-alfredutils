package httpcache_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rwilgaard/go-alfredutils/v2/alfred"
	"github.com/rwilgaard/go-alfredutils/v2/alfred/httpcache"
)

// server counts requests and answers with body, honouring If-None-Match for
// etag when etag is set.
type server struct {
	*httptest.Server
	hits atomic.Int32
	body atomic.Value // string
}

func newServer(t *testing.T, etag string, headers map[string]string) *server {
	t.Helper()
	s := &server{}
	s.body.Store("v1")
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		if etag != "" {
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		_, _ = io.WriteString(w, s.body.Load().(string))
	}))
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, c *http.Client, url string, header ...string) (string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), httpcache.Status(resp)
}

// age makes every stored response look d old.
func age(t *testing.T, store *alfred.Store, d time.Duration) {
	t.Helper()
	entries, err := filepath.Glob(store.Path(filepath.Join(httpcache.DefaultDir, "*")))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no stored responses: %v", err)
	}
	old := time.Now().Add(-d)
	for _, e := range entries {
		if err := os.Chtimes(e, old, old); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFresh(t *testing.T) {
	srv := newServer(t, "", nil)
	store := alfred.NewStore(t.TempDir())
	c := httpcache.New(store, time.Minute)

	if body, status := get(t, c, srv.URL); body != "v1" || status != "" {
		t.Errorf("first get = %q, %q; want v1 from the server", body, status)
	}
	srv.body.Store("v2")
	if body, status := get(t, c, srv.URL); body != "v1" || status != httpcache.Fresh {
		t.Errorf("second get = %q, %q; want stored v1", body, status)
	}
	if n := srv.hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}

	age(t, store, 2*time.Minute)
	if body, status := get(t, c, srv.URL); body != "v2" || status != "" {
		t.Errorf("expired get = %q, %q; want v2 from the server", body, status)
	}
	if body, _ := get(t, c, srv.URL); body != "v2" {
		t.Errorf("get after refetch = %q, want the stored v2", body)
	}
}

func TestRevalidate(t *testing.T) {
	srv := newServer(t, `"abc"`, nil)
	store := alfred.NewStore(t.TempDir())
	c := httpcache.New(store, time.Minute)

	get(t, c, srv.URL)
	age(t, store, 2*time.Minute)
	if body, status := get(t, c, srv.URL); body != "v1" || status != httpcache.Revalidated {
		t.Errorf("get = %q, %q; want v1 revalidated", body, status)
	}
	// The 304 renewed the entry, so the next get makes no request.
	if _, status := get(t, c, srv.URL); status != httpcache.Fresh {
		t.Errorf("get after 304 = %q, want fresh", status)
	}
	if n := srv.hits.Load(); n != 2 {
		t.Errorf("server hits = %d, want 2", n)
	}
	// no-cache from the caller revalidates even a fresh entry.
	if _, status := get(t, c, srv.URL, "Cache-Control", "no-cache"); status != httpcache.Revalidated {
		t.Errorf("no-cache get = %q, want revalidated", status)
	}
}

func TestStaleIfError(t *testing.T) {
	srv := newServer(t, "", nil)
	store := alfred.NewStore(t.TempDir())
	tr := &httpcache.Transport{Store: store, MaxAge: time.Minute, StaleIfError: true}
	c := &http.Client{Transport: tr}

	get(t, c, srv.URL)
	age(t, store, time.Hour)
	srv.Close()
	if body, status := get(t, c, srv.URL); body != "v1" || status != httpcache.Stale {
		t.Errorf("get from a down server = %q, %q; want stale v1", body, status)
	}

	tr.StaleIfError = false
	if _, err := c.Get(srv.URL); err == nil {
		t.Error("get from a down server without StaleIfError succeeded")
	}
}

func TestNotStored(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		req     []string
		method  string
	}{
		{name: "no-store response", headers: map[string]string{"Cache-Control": "no-store"}},
		{name: "range request", req: []string{"Range", "bytes=0-1"}},
		{name: "post", method: http.MethodPost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, "", tt.headers)
			c := httpcache.New(alfred.NewStore(t.TempDir()), time.Minute)
			for range 2 {
				req, err := http.NewRequest(tt.method, srv.URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i+1 < len(tt.req); i += 2 {
					req.Header.Set(tt.req[i], tt.req[i+1])
				}
				resp, err := c.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close() //nolint:errcheck,gosec // read-only body
			}
			if n := srv.hits.Load(); n != 2 {
				t.Errorf("server hits = %d, want 2", n)
			}
		})
	}
}

func TestKeyedByAuthorization(t *testing.T) {
	srv := newServer(t, "", nil)
	store := alfred.NewStore(t.TempDir())
	c := httpcache.New(store, time.Minute)

	get(t, c, srv.URL, "Authorization", "Bearer one")
	if _, status := get(t, c, srv.URL, "Authorization", "Bearer two"); status != "" {
		t.Errorf("other account got a stored response (%s)", status)
	}
	if _, status := get(t, c, srv.URL, "Authorization", "Bearer one"); status != httpcache.Fresh {
		t.Errorf("same account status = %q, want fresh", status)
	}
	entries, _ := filepath.Glob(store.Path(filepath.Join(httpcache.DefaultDir, "*")))
	for _, e := range entries {
		data, err := os.ReadFile(e)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "Bearer") {
			t.Errorf("%s holds the Authorization header", e)
		}
	}

	tr := c.Transport.(*httpcache.Transport)
	if err := tr.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, status := get(t, c, srv.URL, "Authorization", "Bearer one"); status != "" {
		t.Errorf("get after Clear = %q, want from the server", status)
	}
}
