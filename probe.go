package main

import (
	"bytes"
	"crypto/tls"
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
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
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
		return probeLiveness(source, timeout, retries, retryDelay)
	}
	return probeWithRetries(target, source, timeout, retries, retryDelay)
}

func probeLiveness(source BookSource, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	keywords := searchKeywords(source)
	keyword := "我"
	if len(keywords) > 0 {
		keyword = keywords[0]
	}
	target := ProbeTargetFromBookSourceKeyword(source, keyword)
	if target.URL == "" {
		return ProbeResult{Keyword: keyword, Error: "empty probe target"}
	}
	res := probeWithRetries(target, source, timeout, retries, retryDelay)
	res.Keyword = keyword
	if res.OK {
		res.SampleTotal = 1
		res.SampleSuccesses = 1
	}
	return res
}

func probeRandomKeywordSearch(source BookSource, previous ProbeResult, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
	keywords := searchKeywords(source)
	result := previous
	// 重置 SampleTotal 为实际唯一关键词数（清除旧版本虚增）
	result.SampleTotal = len(result.SampleKeywords)

	probesPerCycle := 10
	if len(previous.SampleKeywords) >= len(keywords) {
		probesPerCycle = 1
	}

	var bestDetail ProbeResult
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
			if bestDetail.ProbeURL == "" || res.MS < bestDetail.MS {
				bestDetail = res
			}
		}
		result.SampleKeywords = appendUniqueString(result.SampleKeywords, keyword)
		result.SampleResults = append(result.SampleResults, sampleResultFromProbe(keyword, res))
	}
	if bestDetail.ProbeURL != "" {
		result.DetailOK = bestDetail.DetailOK
		result.ContentOK = bestDetail.ContentOK
		result.ChineseChars = bestDetail.ChineseChars
		result.ExtractedName = bestDetail.ExtractedName
		result.ExtractedAuthor = bestDetail.ExtractedAuthor
		result.ExtractedLastChapter = bestDetail.ExtractedLastChapter
		result.ExtractedKind = bestDetail.ExtractedKind
		result.ExtractedWords = bestDetail.ExtractedWords
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
	result.rawBody = data
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

						// Find chapter link from detail page: first try source rules, fall back to heuristic
						if detailRes.OK && len(detailRes.rawBody) > 200 {
							result.ContentOK = verifyContentFromDetail(detailRes.rawBody, detailURL, source, timeout)
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
	// Try UTF-8 first
	text := string(data)
	count := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			count++
		}
	}
	if count > 0 {
		return count
	}
	// Fallback: try GBK decoding (common for Chinese novel sites without charset header)
	decoder := simplifiedchinese.GBK.NewDecoder()
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), decoder))
	if err == nil {
		for _, r := range string(decoded) {
			if r >= 0x4E00 && r <= 0x9FFF {
				count++
			}
		}
	}
	return count
}

func verifyContentFromDetail(detailBody []byte, detailURL string, source BookSource, timeout time.Duration) bool {
	detailHTML := string(detailBody)

	// Try source's ruleToc CSS selectors first
	ruleToc, hasToc := source["ruleToc"].(map[string]any)
	if hasToc {
		chapterList := strings.TrimSpace(stringField(ruleToc, "chapterList"))
		if chapterList != "" && !strings.HasPrefix(chapterList, "@js:") && !strings.HasPrefix(chapterList, "<js>") {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(detailHTML))
			if err == nil {
				sel := doc.Find(cssSelectorFromRule(chapterList))
				if sel.Length() > 0 {
					chapterURLRule := strings.TrimSpace(stringField(ruleToc, "chapterUrl"))
					firstItem := sel.First()
					chapterURL := extractHTMLField(firstItem, chapterURLRule)
					if chapterURL != "" {
						fullURL := resolveURL(chapterURL, detailURL)
						if fullURL != "" {
							if fetchAndCheckContent(fullURL, source, timeout) {
								return true
							}
						}
					}
				}
			}
		}
	}

	// Fallback: heuristic patterns on detail page HTML
	linkPat := regexp.MustCompile(`<a\s+[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	links := linkPat.FindAllStringSubmatch(detailHTML, -1)

	navWords := []string{"首页", "分类", "排行", "书架", "返回", "登录", "注册", "上一页", "下一页"}
	isNav := func(text, href string) bool {
		for _, w := range navWords {
			if strings.Contains(text, w) || strings.Contains(href, w) {
				return true
			}
		}
		return false
	}

	// Pattern 1: "开始阅读" link
	for _, m := range links {
		href := m[1]
		text := stripTagsRegex(m[2])
		if strings.Contains(text, "开始") || strings.Contains(text, "阅读") {
			if !strings.HasPrefix(href, "javascript") && !strings.HasPrefix(href, "#") && href != "" {
				fullURL := resolveURL(href, detailURL)
				if fullURL != "" && fetchAndCheckContent(fullURL, source, timeout) {
					return true
				}
			}
		}
	}

	// Pattern 2: "第X章" links
	for _, m := range links {
		href := m[1]
		text := stripTagsRegex(m[2])
		if strings.Contains(text, "第") && (strings.Contains(text, "章") || strings.Contains(text, "节") || strings.Contains(text, "回")) {
			if isNav(text, href) || strings.HasPrefix(href, "javascript") || strings.HasPrefix(href, "#") {
				continue
			}
			fullURL := resolveURL(href, detailURL)
			if fullURL != "" && fetchAndCheckContent(fullURL, source, timeout) {
				return true
			}
		}
	}

	// Pattern 3: /数字/数字/数字.html chapter link pattern
	for _, m := range links {
		href := m[1]
		text := stripTagsRegex(m[2])
		if isNav(text, href) || strings.HasPrefix(href, "javascript") || strings.HasPrefix(href, "#") {
			continue
		}
		if regexp.MustCompile(`/\d+[/_]\d+/\d+\.html`).MatchString(href) {
			fullURL := resolveURL(href, detailURL)
			if fullURL != "" && fetchAndCheckContent(fullURL, source, timeout) {
				return true
			}
		}
	}

	return false
}

func fetchAndCheckContent(url string, source BookSource, timeout time.Duration) bool {
	cr := probeWithRetries(ProbeTarget{URL: url, Method: "GET"}, nil, timeout, 1, 0)
	if !cr.OK || len(cr.rawBody) < 200 {
		return false
	}

	bodyStr := string(cr.rawBody)

	// Try ruleContent.content CSS selector first
	ruleContent, hasContent := source["ruleContent"].(map[string]any)
	if hasContent {
		contentRule := strings.TrimSpace(stringField(ruleContent, "content"))
		if contentRule != "" && !strings.HasPrefix(contentRule, "@js:") && !strings.HasPrefix(contentRule, "<js>") {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
			if err == nil {
				sel := doc.Find(cssSelectorFromRule(contentRule))
				if sel.Length() > 0 {
					cn := countChineseCharsInString(sel.Text())
					return cn > 200
				}
			}
		}
	}

	// Fallback: 全文中文数足够就算可读
	return countChineseChars(cr.rawBody) > 500
}

func countChineseCharsInString(s string) int {
	count := 0
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			count++
		}
	}
	return count
}

func stripTagsRegex(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, ""))
}
