package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var yckContentLinkRE = regexp.MustCompile(`/yuedu/(shuyuan|shuyuans)/content/id/(\d+)\.html`)

func fetchRemoteSources(ctx context.Context, cfg Config) ([]BookSource, error) {
	all := []BookSource{}
	for _, listingURL := range cfg.RemoteURLs {
		jsonURLs, err := discoverYCKJSONURLs(ctx, listingURL, remoteLimitForURL(cfg, listingURL))
		if err != nil {
			return all, err
		}
		for _, jsonURL := range jsonURLs {
			sources, err := fetchBookSourceJSON(ctx, jsonURL)
			if err != nil {
				fmt.Printf("remote source skipped %s: %v\n", jsonURL, err)
				continue
			}
			all = append(all, sources...)
		}
	}
	return all, nil
}

func remoteLimitForURL(cfg Config, listingURL string) int {
	if strings.Contains(listingURL, "/shuyuans/") {
		return cfg.MaxRemoteCollections
	}
	if strings.Contains(listingURL, "/shuyuan/") {
		return cfg.MaxRemoteBookSources
	}
	return cfg.MaxRemoteItems
}

func discoverYCKJSONURLs(ctx context.Context, listingURL string, limit int) ([]string, error) {
	body, err := fetchText(ctx, listingURL)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(listingURL)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, match := range yckContentLinkRE.FindAllStringSubmatch(body, -1) {
		kind, id := match[1], match[2]
		path := fmt.Sprintf("/yuedu/%s/json/id/%s.json", kind, id)
		jsonURL := base.ResolveReference(&url.URL{Path: path}).String()
		if seen[jsonURL] {
			continue
		}
		seen[jsonURL] = true
		out = append(out, jsonURL)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func fetchBookSourceJSON(ctx context.Context, rawURL string) ([]BookSource, error) {
	body, err := fetchText(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	body = strings.TrimSpace(html.UnescapeString(body))
	var list []BookSource
	if err := json.Unmarshal([]byte(body), &list); err == nil {
		return list, nil
	}
	var single BookSource
	if err := json.Unmarshal([]byte(body), &single); err != nil {
		return nil, err
	}
	return []BookSource{single}, nil
}

func fetchText(ctx context.Context, rawURL string) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 booksource-tool")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s status %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
