package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	repoOwner = "GloriousEggroll"
	repoName  = "proton-ge-custom"
	apiURL    = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases"
)

type Release struct {
	TagName    string    `json:"tag_name"`
	Name       string    `json:"name"`
	Prerelease bool      `json:"prerelease"`
	Assets     []Asset   `json:"assets"`
	Draft      bool      `json:"draft"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Created    time.Time `json:"created_at"`
	Published  time.Time `json:"published_at"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

// FetchAllReleases paginates through GitHub releases and returns non-draft ones.
func FetchAllReleases(ctx context.Context) ([]Release, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	page := 1
	perPage := 100

	var all []Release
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		q := req.URL.Query()
		q.Set("per_page", fmt.Sprint(perPage))
		q.Set("page", fmt.Sprint(page))
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "ProtonGE-Manager/1.0 (+fyne)")
		if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden {
				err = fmt.Errorf("GitHub API returned 403 (rate limited?). Try setting GITHUB_TOKEN")
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				err = fmt.Errorf("GitHub API error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
				return
			}
			var rels []Release
			if derr := json.NewDecoder(resp.Body).Decode(&rels); derr != nil {
				err = derr
				return
			}
			if len(rels) == 0 {
				return
			}
			all = append(all, rels...)
		}()
		if err != nil {
			return nil, err
		}
		if len(all) < page*perPage {
			break
		}
		page++
	}

	// Filter: skip drafts; require a tarball asset to be useful.
	out := all[:0]
	for _, r := range all {
		if r.Draft {
			continue
		}
		if _, ok := PickLinuxTarball(r.Assets); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func PickLinuxTarball(assets []Asset) (Asset, bool) {
	// Prefer .tar.gz with ge-proton in name; fallback to any .tar.gz
	var cands []Asset
	for _, a := range assets {
		if strings.HasSuffix(strings.ToLower(a.Name), ".tar.gz") && strings.Contains(strings.ToLower(a.Name), "ge-proton") {
			cands = append(cands, a)
		}
	}
	if len(cands) == 0 {
		for _, a := range assets {
			if strings.HasSuffix(strings.ToLower(a.Name), ".tar.gz") {
				cands = append(cands, a)
			}
		}
	}
	if len(cands) == 0 {
		return Asset{}, false
	}
	slices.SortFunc(cands, func(a, b Asset) int {
		switch {
		case a.Size == b.Size:
			return 0
		case a.Size < b.Size:
			return 1
		default:
			return -1
		}
	})
	return cands[0], true
}
