// Package scan enumerates repositories from source hosts such as GitHub orgs.
package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

// Repo identifies a repository to fetch and index.
type Repo struct {
	Owner         string
	Name          string
	FullName      string // "owner/name"
	CloneURL      string
	DefaultBranch string
	Private       bool
}

// GithubClient enumerates repos in a GitHub org or user account.
type GithubClient struct {
	HTTP  *http.Client
	Token string
}

// NewGithubClient returns a client. If token is empty, requests are anonymous.
func NewGithubClient(token string) *GithubClient {
	return &GithubClient{HTTP: http.DefaultClient, Token: token}
}

// TokenFromGHCLI reads a token from the `gh` CLI if present.
func TokenFromGHCLI() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetRepo fetches metadata for a single repo identified as "owner/name".
func (c *GithubClient) GetRepo(ctx context.Context, fullName string) (Repo, error) {
	data, err := c.get(ctx, "https://api.github.com/repos/"+fullName)
	if err != nil {
		return Repo{}, fmt.Errorf("scan: get repo %s: %w", fullName, err)
	}
	var r struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
		CloneURL      string `json:"clone_url"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Repo{}, fmt.Errorf("scan: parse repo %s: %w", fullName, err)
	}
	return Repo{
		Owner:         r.Owner.Login,
		Name:          r.Name,
		FullName:      r.FullName,
		CloneURL:      r.CloneURL,
		DefaultBranch: r.DefaultBranch,
		Private:       r.Private,
	}, nil
}

// ListRepos returns every repo in a GitHub org or user account. It auto-detects
// which by probing /orgs/<name>. Paginates through all pages.
func (c *GithubClient) ListRepos(ctx context.Context, ownerOrOrg string) ([]Repo, error) {
	isOrg := true
	if _, err := c.get(ctx, fmt.Sprintf("https://api.github.com/orgs/%s", ownerOrOrg)); err != nil {
		isOrg = false
	}

	var all []Repo
	for page := 1; ; page++ {
		var url string
		if isOrg {
			url = fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&type=all&page=%d", ownerOrOrg, page)
		} else {
			url = fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100&type=all&page=%d", ownerOrOrg, page)
		}
		data, err := c.get(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("scan: list repos: %w", err)
		}
		var raw []struct {
			Name          string `json:"name"`
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			Private       bool   `json:"private"`
			CloneURL      string `json:"clone_url"`
			Owner         struct {
				Login string `json:"login"`
			} `json:"owner"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("scan: parse repos page %d: %w", page, err)
		}
		if len(raw) == 0 {
			break
		}
		for _, r := range raw {
			all = append(all, Repo{
				Owner:         r.Owner.Login,
				Name:          r.Name,
				FullName:      r.FullName,
				CloneURL:      r.CloneURL,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
			})
		}
	}
	return all, nil
}

func (c *GithubClient) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "token "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github %s: %d %s", url, resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}
