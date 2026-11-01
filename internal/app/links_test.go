package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Missing pages are reported; pages that exist, or refuse robots, aren't.
// When nothing answers at all, the computer is offline and nothing is reported.
func TestCheckLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/robots-no":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	bad := checkLinks(context.Background(), []string{srv.URL + "/doc", srv.URL + "/missing", srv.URL + "/robots-no", srv.URL + "/doc"})
	if len(bad) != 1 || !strings.Contains(bad[0], "/missing returns 404") {
		t.Fatalf("bad = %q", bad)
	}

	// A closed port refuses every connection: as if offline.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if bad := checkLinks(context.Background(), []string{closed.URL + "/a", closed.URL + "/b"}); len(bad) != 0 {
		t.Fatalf("offline reported %q", bad)
	}

	if got := markdownLinks("See [the spec](https://go.dev/ref/spec#Errors) and [blog](http://x.org/a)."); len(got) != 2 || got[0] != "https://go.dev/ref/spec#Errors" {
		t.Fatalf("markdownLinks = %q", got)
	}
}
