package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
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
	keyword := chooseRandomProbeKeyword(keywords, previous)
	target := ProbeTargetFromBookSourceKeyword(source, keyword)
	if target.URL == "" {
		result := previous
		result.Keyword = keyword
		result.OK = false
		result.Error = "empty probe target"
		result.SampleTotal++
		result.SampleKeywords = appendUniqueString(result.SampleKeywords, keyword)
		result.SampleResults = append(result.SampleResults, ProbeSampleResult{Keyword: keyword, Error: result.Error})
		return result
	}
	result := probeWithRetries(target, source, timeout, retries, retryDelay)
	result.Keyword = keyword
	result.SampleTotal = previous.SampleTotal + 1
	result.SampleSuccesses = previous.SampleSuccesses
	if result.OK {
		result.SampleSuccesses++
	}
	result.SampleKeywords = appendUniqueString(append([]string{}, previous.SampleKeywords...), keyword)
	result.SampleResults = append(append([]ProbeSampleResult{}, previous.SampleResults...), sampleResultFromProbe(keyword, result))
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
	if resp.StatusCode == http.StatusOK {
		validation := validateSearchResponse(source, data, time.Since(started).Milliseconds())
		result.ValidationLevel = validation.Level
		result.ValidationGrade = validation.Grade
		result.ValidationNotes = validation.Notes
		result.OK = validation.OK
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
