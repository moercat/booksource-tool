package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
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
				res := ProbeWithSource(job.job.Target, job.job.Source, timeout, retries, retryDelay)
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
	return ProbeWithSource(target, nil, timeout, retries, retryDelay)
}

func ProbeWithSource(target ProbeTarget, source BookSource, timeout time.Duration, retries int, retryDelay time.Duration) ProbeResult {
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
