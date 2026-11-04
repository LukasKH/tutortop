package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"time"
)

// brokenLinks opens the links a tool call cites and returns an error naming
// the ones that don't exist, so the tutor fixes them and calls tool again.
func (a *App) brokenLinks(ctx context.Context, tool string, urls []string) error {
	bad := a.linkCheck(ctx, urls)
	if len(bad) == 0 {
		return nil
	}
	msg := "These links don't open:\n"
	for _, b := range bad {
		msg += "- " + b + "\n"
	}
	return fmt.Errorf("%sFind the right pages (open them to check), then call %s again", msg, tool)
}

// checkLinks opens every link and reports the ones that don't exist (404 or
// 410, or a site that doesn't exist), so a made-up reference is caught before
// the learner sees it. Pages that refuse robots or time out count as fine.
// When nothing can be reached at all, the computer is probably offline, and
// nothing is reported.
func checkLinks(ctx context.Context, urls []string) []string {
	urls = slices.Compact(slices.Sorted(slices.Values(urls)))
	if len(urls) == 0 {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	var (
		mu          sync.Mutex
		bad         []string
		unreachable int
		wg          sync.WaitGroup
	)
	for _, u := range urls {
		wg.Go(func() {
			problem, reached := checkLink(ctx, client, u)
			mu.Lock()
			defer mu.Unlock()
			if !reached {
				unreachable++
			}
			if problem != "" {
				bad = append(bad, problem)
			}
		})
	}
	wg.Wait()
	if unreachable == len(urls) {
		return nil
	}
	slices.Sort(bad)
	return bad
}

// checkLink reports a problem with one link, and whether its site answered.
func checkLink(ctx context.Context, client *http.Client, u string) (problem string, reached bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Sprintf("%s isn't a valid link", u), true
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Tutortop link check)")
	req.Header.Set("Accept", "text/html,*/*")
	resp, err := client.Do(req)
	if err != nil {
		if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && dnsErr.IsNotFound {
			// The name server answered, so the network is up.
			return fmt.Sprintf("%s: there's no such site", u), true
		}
		return "", false
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return fmt.Sprintf("%s returns %s", u, resp.Status), true
	}
	return "", true
}

var markdownLink = regexp.MustCompile(`\]\((https?://[^)\s]+)\)`)

// markdownLinks lists the links in markdown text.
func markdownLinks(md string) []string {
	var out []string
	for _, m := range markdownLink.FindAllStringSubmatch(md, -1) {
		out = append(out, m[1])
	}
	return out
}
