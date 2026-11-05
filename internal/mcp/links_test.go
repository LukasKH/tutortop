package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// Missing pages and sites are reported; pages that refuse robots or don't
// answer in time are not, and nothing is reported offline.
func TestLinkCheckerReportsMissingPages(t *testing.T) {
	t.Run("online", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/missing":
				http.NotFound(w, r)
			case "/gone":
				w.WriteHeader(http.StatusGone)
			case "/robots":
				w.WriteHeader(http.StatusForbidden)
			}
		}))
		defer srv.Close()
		served := srv.Client().Transport
		lc := NewLinkChecker(roundTripper(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Host {
			case "nosuch.example":
				return nil, &net.DNSError{Err: "no such host", Name: r.URL.Host, IsNotFound: true}
			case "slow.example":
				return nil, context.DeadlineExceeded
			}
			return served.RoundTrip(r)
		}))

		got := lc.broken(context.Background(), []string{
			srv.URL + "/ok", srv.URL + "/missing", srv.URL + "/gone", srv.URL + "/robots", srv.URL + "/missing",
			"https://nosuch.example/page", "https://slow.example/page",
		})
		want := []string{
			"https://nosuch.example/page: there's no such site",
			srv.URL + "/gone returns 410 Gone",
			srv.URL + "/missing returns 404 Not Found",
		}
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("offline", func(t *testing.T) {
		lc := NewLinkChecker(roundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network is unreachable")
		}))
		if got := lc.broken(context.Background(), []string{"https://go.dev/doc/", "https://go.dev/made-up"}); len(got) != 0 {
			t.Fatalf("got %q", got)
		}
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
