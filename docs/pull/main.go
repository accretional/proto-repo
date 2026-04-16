// Command pull fetches https://git-scm.com/docs/git, walks the rendered DOM,
// and writes one CSV per <h3> section containing every /docs anchor found
// inside <dl class="dlist"> or <ul class="ulist"> elements that fall under
// that h3. Output goes to docs/git-links/<h3-id>.csv (resolved relative to
// the current working directory — invoke from the repo root).
package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

const (
	sourceURL = "https://git-scm.com/docs/git"
	baseURL   = "https://git-scm.com"
	outDir    = "docs/git-links"
)

type entry struct{ url, text string }

func main() {
	resp, err := http.Get(sourceURL)
	if err != nil {
		log.Fatalf("GET %s: %v", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("GET %s: status %d", sourceURL, resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		log.Fatalf("parse html: %v", err)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", outDir, err)
	}

	// Per-h3 entries, in document order.
	bySection := map[string][]entry{}
	var order []string
	currentFrag := "_top" // catches lists that appear before any <h3>

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h3":
				if id := getAttr(n, "id"); id != "" {
					currentFrag = id
				} else if t := textOf(n); t != "" {
					currentFrag = slug(t)
				}
			default:
				// Asciidoctor wraps each list in <div class="dlist"><dl>…</dl></div>
				// or <div class="ulist"><ul>…</ul></div>, so match the class on
				// any element rather than insisting on dl/ul.
				if hasClass(n, "dlist") || hasClass(n, "ulist") {
					links := collectDocsLinks(n)
					if len(links) == 0 {
						return
					}
					if _, seen := bySection[currentFrag]; !seen {
						order = append(order, currentFrag)
					}
					bySection[currentFrag] = append(bySection[currentFrag], links...)
					return // already collected every descendant <a>
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(order) == 0 {
		log.Fatalf("no /docs links found inside any dlist/ulist on %s", sourceURL)
	}

	for _, frag := range order {
		path := filepath.Join(outDir, frag+".csv")
		f, err := os.Create(path)
		if err != nil {
			log.Fatalf("create %s: %v", path, err)
		}
		w := csv.NewWriter(f)
		if err := w.Write([]string{"url", "text"}); err != nil {
			log.Fatalf("write header to %s: %v", path, err)
		}
		for _, e := range bySection[frag] {
			if err := w.Write([]string{e.url, e.text}); err != nil {
				log.Fatalf("write row to %s: %v", path, err)
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			log.Fatalf("flush %s: %v", path, err)
		}
		f.Close()
		fmt.Printf("wrote %s (%d links)\n", path, len(bySection[frag]))
	}
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, want string) bool {
	for _, c := range strings.Fields(getAttr(n, "class")) {
		if c == want {
			return true
		}
	}
	return false
}

// textOf returns the concatenated text content of n with whitespace squeezed.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// collectDocsLinks returns every <a href="/docs..."> descendant of n, in
// document order, paired with its trimmed text content.
func collectDocsLinks(n *html.Node) []entry {
	var out []entry
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := getAttr(n, "href")
			if strings.HasPrefix(href, "/docs") {
				if txt := textOf(n); txt != "" {
					out = append(out, entry{url: baseURL + href, text: txt})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// slug fallback: lowercase + non-alphanumerics → '_'. Only used when an h3
// has no explicit id (rare on git-scm.com which generates them from text).
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
