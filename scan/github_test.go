package scan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// rewriteTransport replaces api.github.com in the outgoing request URL
// with the httptest server's host. Lets us exercise ListRepos' URL
// construction and pagination logic without modifying production code.
type rewriteTransport struct {
	base *url.URL
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.base.Scheme
	req.URL.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testClient(t *testing.T, handler http.HandlerFunc) *GithubClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	return &GithubClient{
		HTTP:  &http.Client{Transport: &rewriteTransport{base: u}},
		Token: "test-token",
	}
}

func TestListReposOrgSingePage(t *testing.T) {
	var gotAuth string
	handler := func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/orgs/acme":
			// Org probe succeeds.
			fmt.Fprintln(w, `{"login":"acme"}`)
		case r.URL.Path == "/orgs/acme/repos" && r.URL.Query().Get("page") == "1":
			fmt.Fprintln(w, `[
				{"name":"alpha","full_name":"acme/alpha","clone_url":"https://x/acme/alpha.git","owner":{"login":"acme"}},
				{"name":"beta","full_name":"acme/beta","clone_url":"https://x/acme/beta.git","fork":true,"archived":true,"owner":{"login":"acme"}}
			]`)
		default:
			// Page 2 empty → pagination terminates.
			fmt.Fprintln(w, `[]`)
		}
	}
	c := testClient(t, handler)
	got, err := c.ListRepos(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[0].CloneURL != "https://x/acme/alpha.git" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if !got[1].Fork || !got[1].Archived {
		t.Errorf("got[1] fork/archived not parsed: %+v", got[1])
	}
	if !strings.HasPrefix(gotAuth, "token ") {
		t.Errorf("Authorization header = %q, want token prefix", gotAuth)
	}
}

func TestListReposUserFallback(t *testing.T) {
	var probed, listed bool
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/orgs/solo":
			// 404 signals "not an org" — ListRepos should fall back to /users.
			probed = true
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/users/solo/repos" && r.URL.Query().Get("page") == "1":
			listed = true
			fmt.Fprintln(w, `[{"name":"dotfiles","full_name":"solo/dotfiles","clone_url":"https://x/solo/dotfiles.git","owner":{"login":"solo"}}]`)
		default:
			// Pagination-terminating empty page.
			fmt.Fprintln(w, `[]`)
		}
	}
	c := testClient(t, handler)
	got, err := c.ListRepos(context.Background(), "solo")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if !probed {
		t.Error("expected /orgs/solo probe")
	}
	if !listed {
		t.Error("expected /users/solo/repos call")
	}
	if len(got) != 1 || got[0].Name != "dotfiles" {
		t.Errorf("got %+v, want 1 repo 'dotfiles'", got)
	}
}

func TestListReposPagination(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/orgs/big":
			fmt.Fprintln(w, `{}`)
		case r.URL.Path == "/orgs/big/repos":
			switch r.URL.Query().Get("page") {
			case "1":
				fmt.Fprintln(w, `[{"name":"one","owner":{"login":"big"}}]`)
			case "2":
				fmt.Fprintln(w, `[{"name":"two","owner":{"login":"big"}}]`)
			default:
				fmt.Fprintln(w, `[]`)
			}
		}
	}
	c := testClient(t, handler)
	got, err := c.ListRepos(context.Background(), "big")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(got) != 2 || got[0].Name != "one" || got[1].Name != "two" {
		t.Errorf("pagination order/count wrong: %+v", got)
	}
}

func TestListReposErrorsOnBadStatus(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/denied":
			// Pretend this is an org so we go down the repos path, then 403.
			fmt.Fprintln(w, `{}`)
		default:
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintln(w, `{"message":"rate limited"}`)
		}
	}
	c := testClient(t, handler)
	_, err := c.ListRepos(context.Background(), "denied")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want to mention 403", err)
	}
}
