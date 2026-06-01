package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func ProbeAll(probeTargets map[string]ProbeTarget, workers int, timeout time.Duration, retries int, retryDelay time.Duration) map[string]ProbeResult {
	jobs := make(map[string]ProbeJob, len(probeTargets))
	for key, target := range probeTargets {
		jobs[key] = ProbeJob{Target: target}
	}
	return ProbeAllJobs(jobs, workers, timeout, retries, retryDelay)
}

func ProbeAllJobs(probeJobs map[string]ProbeJob, workers int, timeout time.Duration, retries int, retryDelay time.Duration) map[string]ProbeResult {
	if workers <= 0 {
		workers = 1
	}
	results := make(map[string]ProbeResult, len(probeJobs))
	jobs := make(chan struct {
		key string
		job ProbeJob
	})
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				res := ProbeWithSource(job.job.Target, job.job.Source, job.job.Previous, timeout, retries, retryDelay)
				mu.Lock()
				results[job.key] = res
				mu.Unlock()
			}
		}()
	}
	for key, job := range probeJobs {
		jobs <- struct {
			key string
			job ProbeJob
		}{key: key, job: job}
	}
	close(jobs)
	wg.Wait()
	return results
}

func Probe(target ProbeTarget, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	return ProbeWithSource(target, nil, ProbeResult{}, timeout, retries, retryDelay)
}

func ProbeWithSource(target ProbeTarget, source BookSource, previous ProbeResult, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	if len(source) > 0 {
		return probeRandomKeywordSearch(source, previous, timeout, retries, retryDelay)
	}
	return probeWithRetries(target, source, timeout, retries, retryDelay)
}

func probeRandomKeywordSearch(source BookSource, previous ProbeResult, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	keywords := searchKeywords(source)
	result := previous

	// Probe 3 keywords per cycle to speed up first pass
	probesPerCycle := 3
	if len(previous.SampleKeywords) >= len(keywords) {
		probesPerCycle = 1 // all tested, maintenance mode
	}

	for i := 0; i < probesPerCycle; i++ {
		keyword := chooseRandomProbeKeyword(keywords, result)
		target := ProbeTargetFromBookSourceKeyword(source, keyword)
		result.SampleTotal++
		if target.URL == "" {
			result.OK = false
			result.Error = "empty probe target"
			result.SampleKeywords = appendUniqueString(result.SampleKeywords, keyword)
			result.SampleResults = append(result.SampleResults, ProbeSampleResult{Keyword: keyword, Error: result.Error})
			continue
		}
		res := probeWithRetries(target, source, timeout, retries, retryDelay)
		res.Keyword = keyword
		if res.OK {
			result.SampleSuccesses++
		}
		result.SampleKeywords = appendUniqueString(result.SampleKeywords, keyword)
		result.SampleResults = append(result.SampleResults, sampleResultFromProbe(keyword, res))
	}
	return result
}

func chooseRandomProbeKeyword(keywords []string, previous ProbeResult) string {
	if len(keywords) == 0 {
		return "我"
	}
	seen := map[string]bool{}
	for _, keyword := range previous.SampleKeywords {
		seen[keyword] = true
	}
	candidates := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if !seen[keyword] {
			candidates = append(candidates, keyword)
		}
	}
	if len(candidates) == 0 {
		candidates = keywords
	}
	seed := time.Now().UnixNano() + int64(previous.SampleTotal)*131 + int64Hash(strings.Join(previous.SampleKeywords, "|"))
	return candidates[rand.New(rand.NewSource(seed)).Intn(len(candidates))]
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func probeWithSourceStages(target ProbeTarget, source BookSource, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	keywords := searchKeywords(source)
	if len(keywords) == 0 {
		keywords = []string{"我"}
	}
	primaryKeyword := keywords[0]
	primary := probeWithRetries(target, source, timeout, retries, retryDelay)
	primary.Keyword = primaryKeyword
	primary.SampleTotal = 1
	if primary.OK {
		primary.SampleSuccesses = 1
	}
	primary.SampleKeywords = []string{primaryKeyword}
	primary.SampleResults = []ProbeSampleResult{sampleResultFromProbe(primaryKeyword, primary)}
	if !primary.OK {
		return primary
	}
	if len(keywords) == 1 {
		return primary
	}

	samples := make([]ProbeSampleResult, 0, len(keywords))
	samples = append(samples, primary.SampleResults[0])
	best := primary
	successes := 1
	for _, keyword := range keywords[1:] {
		target := ProbeTargetFromBookSourceKeyword(source, keyword)
		if target.URL == "" {
			samples = append(samples, ProbeSampleResult{Keyword: keyword, Error: "empty probe target"})
			continue
		}
		res := probeWithRetries(target, source, timeout, 1, 0)
		res.Keyword = keyword
		samples = append(samples, sampleResultFromProbe(keyword, res))
		if res.OK {
			successes++
			if !best.OK || betterProbeResult(res, best) {
				best = res
			}
		}
	}
	best.SampleTotal = len(samples)
	best.SampleSuccesses = successes
	best.SampleKeywords = keywords
	best.SampleResults = samples
	if successes > 0 {
		best.OK = true
	}
	return best
}

func probeWithRetries(target ProbeTarget, source BookSource, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	if retries <= 0 {
		retries = 1
	}
	var last ProbeResult
	for attempt := 1; attempt <= retries; attempt++ {
		last = probeOnce(target, source, timeout)
		last.Attempts = attempt
		if last.OK {
			return last
		}
		if attempt < retries && retryDelay > 0 {
			time.Sleep(retryDelay)
		}
	}
	return last
}

func sampleResultFromProbe(keyword string, res ProbeResult) ProbeSampleResult {
	return ProbeSampleResult{Keyword: keyword, ProbeURL: res.ProbeURL, OK: res.OK, Status: res.Status, Error: res.Error, MS: res.MS, BodyBytes: res.BodyBytes, ValidationLevel: res.ValidationLevel, ValidationGrade: res.ValidationGrade, ValidationNotes: res.ValidationNotes}
}

func betterProbeResult(candidate, current ProbeResult) bool {
	if candidate.ValidationLevel != current.ValidationLevel {
		return candidate.ValidationLevel > current.ValidationLevel
	}
	if current.MS == 0 {
		return true
	}
	return candidate.MS > 0 && candidate.MS < current.MS
}

func probeOnce(target ProbeTarget, source BookSource, timeout time.Duration) ProbeResult {
	started := time.Now()
	method := strings.ToUpper(strings.TrimSpace(target.Method))
	if method == "" {
		method = http.MethodGet
	}
	result := ProbeResult{ProbeURL: target.URL, Method: method, Body: target.Body, Domain: domainOf(target.URL)}
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if method == http.MethodGet && source == nil {
		if resp, err := doProbeRequest(client, http.MethodHead, target.URL, ""); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				result.OK, result.Status, result.FinalURL = true, resp.StatusCode, resp.Request.URL.String()
				result.MS = time.Since(started).Milliseconds()
				return result
			}
			result.Status, result.FinalURL = resp.StatusCode, resp.Request.URL.String()
		}
	}
	resp, err := doProbeRequest(client, method, target.URL, target.Body)
	if err != nil {
		result.Error = fmt.Sprintf("%T: %.200s", err, err.Error())
		result.MS = time.Since(started).Milliseconds()
		return result
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProbeBodyBytes))
	if readErr != nil {
		result.Error = fmt.Sprintf("%T: %.200s", readErr, readErr.Error())
	}
	result.Status, result.FinalURL = resp.StatusCode, resp.Request.URL.String()
	result.BodyBytes = len(data)
	result.ChineseChars = countChineseChars(data)
	if resp.StatusCode == http.StatusOK {
		validation := validateSearchResponse(source, data, time.Since(started).Milliseconds())
		result.ValidationLevel = validation.Level
		result.ValidationGrade = validation.Grade
		result.ValidationNotes = validation.Notes
		result.OK = validation.OK

		// Extract book metadata from search results
		if result.OK && len(source) > 0 {
			ruleSearch, _ := source["ruleSearch"].(map[string]any)
			isJSON := jsonLooksValid(string(data))
			books := extractSearchBooks(data, ruleSearch, isJSON)
			if len(books) > 0 {
				book := books[0]
				result.ExtractedName = book["name"]
				result.ExtractedAuthor = book["author"]
				result.ExtractedLastChapter = book["lastChapter"]
				result.ExtractedKind = book["kind"]
				result.ExtractedWords = book["wordCount"]

				// Try to verify the first book's detail page is accessible
				if book["bookUrl"] != "" {
					detailURL := resolveURL(book["bookUrl"], stringField(source, "bookSourceUrl"))
					if detailURL != "" {
						detailRes := probeWithRetries(ProbeTarget{URL: detailURL, Method: "GET"}, nil, timeout, 1, 0)
						result.DetailOK = detailRes.OK

						// Try to find content using source's own rules
						if detailRes.OK && len(detailRes.rawBody) > 200 {
							ruleBookInfo, _ := source["ruleBookInfo"].(map[string]any)
							ruleToc, _ := source["ruleToc"].(map[string]any)

							// Determine TOC URL: from ruleBookInfo.tocUrl or use detail URL
							tocURL := ""
							tocRule := strings.TrimSpace(stringField(ruleBookInfo, "tocUrl"))
							if tocRule != "" && !strings.HasPrefix(tocRule, "@js:") && !strings.HasPrefix(tocRule, "<js>") {
								if strings.HasPrefix(tocRule, "http") {
									tocURL = tocRule
								} else {
									// CSS selector to extract tocUrl from detail page
									detailDoc, docErr := goquery.NewDocumentFromReader(strings.NewReader(string(detailRes.rawBody)))
									if docErr == nil {
										tocURL = extractHTMLField(detailDoc.Selection, tocRule)
									}
								}
							}
							if tocURL == "" {
								tocURL = detailURL
							} else {
								tocURL = resolveURL(tocURL, detailURL)
							}

							// Fetch TOC page and try to extract first chapter
							chapterListRule := strings.TrimSpace(stringField(ruleToc, "chapterList"))
							chapterURLRule := strings.TrimSpace(stringField(ruleToc, "chapterUrl"))
							if chapterListRule != "" && !strings.HasPrefix(chapterListRule, "@js:") && !strings.HasPrefix(chapterListRule, "<js>") {
								tocRes := probeWithRetries(ProbeTarget{URL: tocURL, Method: "GET"}, nil, timeout, 1, 0)
								if tocRes.OK && len(tocRes.rawBody) > 200 {
									isJSON := jsonLooksValid(string(tocRes.rawBody))
									if isJSON && strings.HasPrefix(chapterListRule, "$") {
										// JSON path - extract first chapter
										var root any
										if json.Unmarshal(tocRes.rawBody, &root) == nil {
											items := resolveJSONPath(root, chapterListRule)
											if len(items) > 0 {
												if m, ok := items[0].(map[string]any); ok {
													if chapterURLRule != "" && strings.HasPrefix(chapterURLRule, "$") {
														cu := resolveJSONString(m, chapterURLRule)
														if cu != "" {
															fullCU := resolveURL(cu, tocURL)
															if fullCU != "" {
																cr := probeWithRetries(ProbeTarget{URL: fullCU, Method: "GET"}, nil, timeout, 1, 0)
																result.ContentOK = cr.OK && len(cr.rawBody) > 500
															}
														}
													}
												}
											}
										}
									} else {
										// HTML - use CSS selector
										doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(tocRes.rawBody)))
										if err == nil {
											sel := doc.Find(cssSelectorFromRule(chapterListRule))
											if sel.Length() > 0 {
												firstItem := sel.First()
												chapterURL := extractHTMLField(firstItem, chapterURLRule)
												if chapterURL != "" {
													fullCU := resolveURL(chapterURL, tocURL)
													if fullCU != "" {
														cr := probeWithRetries(ProbeTarget{URL: fullCU, Method: "GET"}, nil, timeout, 1, 0)
														result.ContentOK = cr.OK && len(cr.rawBody) > 500
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	} else {
		result.ValidationGrade = "D"
		result.ValidationNotes = []string{"non-200 status"}
	}
	result.MS = time.Since(started).Milliseconds()
	return result
}

func doProbeRequest(client *http.Client, method, probeURL, body string) (*http.Response, error) {
	var reader io.Reader
	if body != "" && method != http.MethodGet && method != http.MethodHead {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, probeURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36")
	if reader != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return client.Do(req)
}

func resolveURL(rawURL, baseURL string) string {
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if strings.HasPrefix(rawURL, "/") {
		parsed, err := url.Parse(baseURL)
		if err != nil {
			return ""
		}
		return parsed.Scheme + "://" + parsed.Host + rawURL
	}
	if idx := strings.LastIndex(baseURL, "/"); idx >= 0 {
		return baseURL[:idx+1] + rawURL
	}
	return baseURL + "/" + rawURL
}

func countChineseChars(data []byte) int {
	count := 0
	for _, b := range string(data) {
		if b >= 0x4E00 && b <= 0x9FFF {
			count++
		}
	}
	return count
}

func findContentURL(body []byte) string {
	text := string(body)
	// Skip non-content file extensions
	skipExt := func(u string) bool {
		for _, ext := range []string{".css", ".js", ".png", ".jpg", ".gif", ".svg", ".ico", ".woff", ".ttf"} {
			if strings.HasSuffix(u, ext) {
				return true
			}
		}
		return false
	}

	// Look for chapter-like URLs
	re := regexp.MustCompile(`href=["']([^"']+)["']`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		u := m[1]
		if skipExt(u) {
			continue
		}
		if strings.Contains(u, "search") || strings.Contains(u, "login") || strings.HasPrefix(u, "#") || strings.HasPrefix(u, "javascript") {
			continue
		}
		// Prefer URLs with chapter indicators
		if strings.Contains(u, "chapter") || strings.Contains(u, "read") || strings.Contains(u, "/book/") || strings.Contains(u, "/novel/") {
			if strings.Count(u, "/") >= 2 {
				return u
			}
		}
		// Or URLs ending with .html and not looking like a category page
		if strings.HasSuffix(u, ".html") && strings.Count(u, "/") >= 2 && !strings.Contains(u, "list") && !strings.Contains(u, "index") {
			return u
		}
	}
	return ""
}
