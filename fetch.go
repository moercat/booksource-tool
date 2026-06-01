package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var yckContentLinkRE = regexp.MustCompile(`/yuedu/(shuyuan|shuyuans)/content/id/(\d+)\.html`)

type remoteFetchState struct {
	Fetched       map[string]int64  `json:"fetched"`
	LastByListing map[string]string `json:"lastByListing"`
}

func fetchRemoteSources(ctx context.Context, cfg Config) ([]BookSource, error) {
	all := []BookSource{}
	state, stateErr := loadRemoteFetchState(cfg)
	if stateErr != nil {
		fmt.Printf("remote fetch state load failed: %v\n", stateErr)
	}
	totalListings := 0
	failedListings := 0
	totalJSON := 0
	successJSON := 0
	skippedJSON := 0
	skippedSeenJSON := 0
	for _, listingURL := range cfg.RemoteURLs {
		totalListings++
		kind := remoteKindForURL(listingURL)
		discoveredJSONURLs, err := discoverYCKJSONURLs(ctx, listingURL)
		if err != nil {
			failedListings++
			fmt.Printf("remote listing failed kind=%s url=%s error=%v\n", kind, listingURL, err)
			fmt.Printf("remote fetch summary listings=%d failedListings=%d discoveredJSON=%d selectedJSON=%d successJSON=%d skippedJSON=%d skippedSeenJSON=%d sources=%d\n", totalListings, failedListings, totalJSON+len(discoveredJSONURLs), totalJSON, successJSON, skippedJSON, skippedSeenJSON, len(all))
			return all, err
		}
		jsonURLs, listingSkippedSeen, listingFetchedBefore, listingBackfillDone := selectNewRemoteJSONURLs(state, listingURL, discoveredJSONURLs, remoteLimitForURL(cfg, listingURL), remoteBackfillLimitForURL(cfg, listingURL))
		skippedSeenJSON += listingSkippedSeen
		listingSources := 0
		listingSuccessJSON := 0
		listingSkippedJSON := 0
		for _, jsonURL := range jsonURLs {
			totalJSON++
			rememberRemoteFetch(state, listingURL, jsonURL)
			sources, err := fetchBookSourceJSON(ctx, jsonURL)
			if err != nil {
				skippedJSON++
				listingSkippedJSON++
				fmt.Printf("remote source skipped %s: %v\n", jsonURL, err)
				continue
			}
			successJSON++
			listingSuccessJSON++
			listingSources += len(sources)
			all = append(all, sources...)
		}
		fmt.Printf("remote listing summary kind=%s url=%s discoveredJSON=%d selectedJSON=%d skippedSeenJSON=%d fetchedBefore=%d backfillLimit=%d backfillDone=%t successJSON=%d skippedJSON=%d sources=%d\n", kind, listingURL, len(discoveredJSONURLs), len(jsonURLs), listingSkippedSeen, listingFetchedBefore, remoteBackfillLimitForURL(cfg, listingURL), listingBackfillDone, listingSuccessJSON, listingSkippedJSON, listingSources)
	}
	if err := saveRemoteFetchState(cfg, state); err != nil {
		fmt.Printf("remote fetch state save failed: %v\n", err)
	}
	fmt.Printf("remote fetch summary listings=%d failedListings=%d selectedJSON=%d successJSON=%d skippedJSON=%d skippedSeenJSON=%d sources=%d\n", totalListings, failedListings, totalJSON, successJSON, skippedJSON, skippedSeenJSON, len(all))
	return all, nil
}

func remoteKindForURL(listingURL string) string {
	if strings.Contains(listingURL, "/shuyuans/") {
		return "collection"
	}
	if strings.Contains(listingURL, "/shuyuan/") {
		return "bookSource"
	}
	return "unknown"
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

func remoteBackfillLimitForURL(cfg Config, listingURL string) int {
	if strings.Contains(listingURL, "/shuyuans/") {
		return cfg.MaxRemoteBackfillCollections
	}
	if strings.Contains(listingURL, "/shuyuan/") {
		return cfg.MaxRemoteBackfillBookSources
	}
	return cfg.MaxRemoteItems
}

func discoverYCKJSONURLs(ctx context.Context, listingURL string) ([]string, error) {
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
	}
	return out, nil
}

func selectNewRemoteJSONURLs(state remoteFetchState, listingURL string, discovered []string, limit, backfillLimit int) ([]string, int, int, bool) {
	if limit <= 0 {
		limit = len(discovered)
	}
	fetchedBefore := fetchedRemoteCountForListing(state, listingURL)
	backfillDone := backfillLimit > 0 && fetchedBefore >= backfillLimit
	candidates := discovered
	if backfillDone && limit < len(candidates) {
		candidates = candidates[:limit]
	}
	selected := []string{}
	skippedSeen := 0
	for _, jsonURL := range candidates {
		if _, ok := state.Fetched[jsonURL]; ok {
			skippedSeen++
			continue
		}
		selected = append(selected, jsonURL)
		if len(selected) >= limit {
			break
		}
	}
	return selected, skippedSeen, fetchedBefore, backfillDone
}

func fetchedRemoteCountForListing(state remoteFetchState, listingURL string) int {
	kind := remoteKindForURL(listingURL)
	count := 0
	for jsonURL := range state.Fetched {
		if remoteKindForURL(jsonURL) == kind {
			count++
		}
	}
	return count
}

func rememberRemoteFetch(state remoteFetchState, listingURL, jsonURL string) {
	if state.Fetched == nil {
		state.Fetched = map[string]int64{}
	}
	if state.LastByListing == nil {
		state.LastByListing = map[string]string{}
	}
	state.Fetched[jsonURL] = time.Now().Unix()
	state.LastByListing[listingURL] = jsonURL
}

func loadRemoteFetchState(cfg Config) (remoteFetchState, error) {
	state := remoteFetchState{Fetched: map[string]int64{}, LastByListing: map[string]string{}}
	data, err := os.ReadFile(remoteFetchStatePath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return remoteFetchState{Fetched: map[string]int64{}, LastByListing: map[string]string{}}, err
	}
	if state.Fetched == nil {
		state.Fetched = map[string]int64{}
	}
	if state.LastByListing == nil {
		state.LastByListing = map[string]string{}
	}
	return state, nil
}

func saveRemoteFetchState(cfg Config, state remoteFetchState) error {
	path := remoteFetchStatePath(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func remoteFetchStatePath(cfg Config) string {
	return filepath.Join(cfg.OutputDir, "remote_fetch_state.json")
}

func fetchBookSourceJSON(ctx context.Context, rawURL string) ([]BookSource, error) {
	body, err := fetchText(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if sources, err := parseBookSourceJSON(body); err == nil {
		return sources, nil
	}
	return parseBookSourceJSON(strings.TrimSpace(html.UnescapeString(body)))
}

func parseBookSourceJSON(body string) ([]BookSource, error) {
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
