package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func run(cfg Config) error {
	setDefaultSearchKeywords(cfg.SearchKeywords)
	logf("config loaded: keywords=%d workers=%d timeout=%v interval=%v\n",
		len(cfg.SearchKeywords), cfg.Workers, cfg.Timeout, cfg.Interval)
	if cfg.WatchDir != "" {
		return runWatch(cfg)
	}
	return runOnce(cfg)
}

func runWatch(cfg Config) error {
	if cfg.MongoURI == "" {
		return errors.New("--watch-dir requires --mongo-uri so historical dedupe can persist")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if err := os.MkdirAll(cfg.WatchDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return err
	}
	ctx := context.Background()
	pool := NewSourcePool(cfg)
	if err := restorePoolFromMongo(cfg, pool); err != nil {
		logf("restore pool failed: %v\n", err)
	}
	lastOutputHash, _ := fileSHA1(filepath.Join(cfg.OutputDir, "usable_dedup_sources.json"))
	nextRemoteFetch := time.Time{}
	for {
		if err := runSchedulerCycle(ctx, cfg, pool, &nextRemoteFetch, &lastOutputHash); err != nil {
			logf("run failed: %v\n", err)
		} else {
			logf("run completed\n")
		}
		time.Sleep(cfg.Interval)
	}
}

func runSchedulerCycle(ctx context.Context, cfg Config, pool *SourcePool, nextRemoteFetch *time.Time, lastOutputHash *string) error {
	// 每轮重新加载配置（关键词热更新等）
	if reloaded := reloadConfig(&cfg); reloaded {
		setDefaultSearchKeywords(cfg.SearchKeywords)
	}
	now := time.Now()
	if poolHasNoSources(pool) || !now.Before(*nextRemoteFetch) {
		sources := []BookSource{}
		specs := effectiveSourceSpecs(cfg)
		localSources, err := loadSourcesFromSpecs(specs, cfg.OutputDir)
		if err != nil {
			return err
		}
		sources = append(sources, localSources...)
		historical, err := loadHistoricalSourcesFromMongo(cfg)
		if err != nil {
			return err
		}
		sources = append(sources, historical...)
		remoteSources, err := fetchRemoteSources(ctx, cfg)
		if err != nil {
			logf("remote fetch failed: %v\n", err)
		} else {
			sources = append(sources, remoteSources...)
		}
		sources = uniqueBookSources(sources)
		added := pool.MergeSources(sources, "scheduler", now)
		logf("source pool refreshed: input=%d added=%d pool=%d\n", len(sources), added, len(pool.Entries))
		*nextRemoteFetch = now.Add(cfg.RemoteFetchInterval)
	}

	dueJobs := pool.DueJobs(now)
	if len(dueJobs) > 0 {
		probeResults := ProbeAllJobs(dueJobs, cfg.Workers, cfg.Timeout, cfg.Retries, cfg.RetryDelay)
		for key, result := range probeResults {
			pool.RecordProbe(key, result, time.Now())
		}
		logf("probe completed: due=%d\n", len(dueJobs))
	}

	usablePath := filepath.Join(cfg.OutputDir, "usable_dedup_sources.json")
	duplicatePath := filepath.Join(cfg.OutputDir, "duplicate_report.json")
	connectivityPath := filepath.Join(cfg.OutputDir, "connectivity_report.json")
	appUsabilityPath := filepath.Join(cfg.OutputDir, "app_usability_report.json")
	summaryPath := filepath.Join(cfg.OutputDir, "summary.json")
	usable := pool.UsableSources()
	outputHash, usableJSON, err := stableJSONHash(usable)
	if err != nil {
		return err
	}
	if outputHash != *lastOutputHash {
		if err := os.WriteFile(usablePath, usableJSON, 0644); err != nil {
			return err
		}
		*lastOutputHash = outputHash
		logf("usable output changed: count=%d hash=%s\n", len(usable), outputHash)
	}
	probeValues := poolProbeValues(pool)
	if err := writeJSON(connectivityPath, probeValues); err != nil {
		return err
	}
	appUsabilityItems := []AppUsabilityReportItem{}
	for _, entry := range pool.Entries {
		if entry.Status == SourceStatusUsable && !shouldExcludeBookSource(entry.Source) {
			appUsabilityItems = append(appUsabilityItems, appUsabilityItem(cleanBookSourceName(entry.Source), entry.LastProbe, true))
		}
	}
	if err := writeJSON(appUsabilityPath, buildAppUsabilityReport(appUsabilityItems)); err != nil {
		return err
	}
	duplicates := poolDuplicateGroups()
	if err := writeJSON(duplicatePath, duplicates); err != nil {
		return err
	}
	summary := pool.Summary(outputHash, map[string]string{"usable": usablePath, "duplicates": duplicatePath, "connectivity": connectivityPath, "appUsability": appUsabilityPath, "summary": summaryPath})
	if err := writeJSON(summaryPath, summary); err != nil {
		return err
	}
	if err := persistPoolToMongo(cfg, pool, summary); err != nil {
		return err
	}
	return nil
}

func poolHasNoSources(pool *SourcePool) bool { return len(pool.Entries) == 0 }

func poolProbeValues(pool *SourcePool) []ProbeResult {
	results := []ProbeResult{}
	for _, entry := range pool.Entries {
		if entry.LastProbeAt > 0 {
			results = append(results, entry.LastProbe)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ProbeURL < results[j].ProbeURL })
	return results
}

func poolDuplicateGroups() []DuplicateGroup { return []DuplicateGroup{} }

func runOnce(cfg Config) error {
	specs := effectiveSourceSpecs(cfg)
	if len(specs) == 0 && cfg.MongoURI == "" {
		return errors.New("input file, --source, --watch-dir file, or --mongo-uri history required")
	}
	sources, err := loadSourcesFromSpecs(specs, cfg.OutputDir)
	if err != nil {
		return err
	}
	if cfg.MongoURI != "" {
		historical, err := loadHistoricalSourcesFromMongo(cfg)
		if err != nil {
			return err
		}
		sources = append(sources, historical...)
	}
	sources = uniqueBookSources(sources)

	groups := map[string][]BookSource{}
	invalid := 0
	for _, source := range sources {
		if shouldExcludeBookSource(source) {
			invalid++
			continue
		}
		source = cleanBookSourceName(source)
		structuralTarget := StructuralProbeTargetFromBookSource(source)
		probeTarget := ProbeTargetFromBookSource(source)
		key := NormalizeProbeTarget(structuralTarget)
		if key == "" || structuralTarget.URL == "" || probeTarget.URL == "" {
			invalid++
			continue
		}
		groups[key] = append(groups[key], source)
	}

	probeJobs := map[string]ProbeJob{}
	for key, items := range groups {
		representative := chooseRepresentative(items)
		probeJobs[key] = ProbeJob{Target: ProbeTargetFromBookSource(representative), Source: representative}
	}
	probeResults := ProbeAllJobs(probeJobs, cfg.Workers, cfg.Timeout, cfg.Retries, cfg.RetryDelay)

	usable := []BookSource{}
	appUsabilityItems := []AppUsabilityReportItem{}
	duplicates := []DuplicateGroup{}
	extraDuplicates := 0
	for key, items := range groups {
		if len(items) > 1 {
			extraDuplicates += len(items) - 1
			duplicates = append(duplicates, DuplicateGroup{NormalizedURL: key, Count: len(items), Names: namesOf(items), RawURLs: rawURLsOf(items)})
		}
		if probeResults[key].OK {
			representative := chooseRepresentative(items)
			usable = append(usable, cleanBookSourceName(representative))
			appUsabilityItems = append(appUsabilityItems, appUsabilityItem(representative, probeResults[key], true))
		}
	}
	sort.Slice(duplicates, func(i, j int) bool { return duplicates[i].Count > duplicates[j].Count })

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return err
	}
	usablePath := filepath.Join(cfg.OutputDir, "usable_dedup_sources.json")
	duplicatePath := filepath.Join(cfg.OutputDir, "duplicate_report.json")
	connectivityPath := filepath.Join(cfg.OutputDir, "connectivity_report.json")
	appUsabilityPath := filepath.Join(cfg.OutputDir, "app_usability_report.json")
	summaryPath := filepath.Join(cfg.OutputDir, "summary.json")
	if err := writeJSON(usablePath, usable); err != nil {
		return err
	}
	if err := writeJSON(duplicatePath, duplicates); err != nil {
		return err
	}
	if err := writeJSON(connectivityPath, values(probeResults)); err != nil {
		return err
	}
	if err := writeJSON(appUsabilityPath, buildAppUsabilityReport(appUsabilityItems)); err != nil {
		return err
	}

	summary := Summary{
		Input:                 cfg.Input,
		Records:               len(sources),
		InvalidRecords:        invalid,
		UniqueNormalizedURLs:  len(groups),
		DuplicateGroups:       len(duplicates),
		ExtraDuplicateRecords: extraDuplicates,
		UsableDedupSources:    len(usable),
		FailedUniqueURLs:      len(groups) - len(usable),
		StatusCounts:          statusCounts(probeResults),
		ErrorCounts:           errorCounts(probeResults),
		Outputs:               map[string]string{"usable": usablePath, "duplicates": duplicatePath, "connectivity": connectivityPath, "appUsability": appUsabilityPath, "summary": summaryPath},
		UpdatedAt:             time.Now().Unix(),
	}
	if err := writeJSON(summaryPath, summary); err != nil {
		return err
	}
	if cfg.MongoURI != "" {
		if err := cacheToMongo(cfg, sources, groups, probeResults, duplicates, summary); err != nil {
			return err
		}
	}
	if cfg.PublishTarget != "" {
		if err := publishFile(usablePath, cfg.PublishTarget); err != nil {
			return err
		}
	}
	encoded, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func uniqueBookSources(sources []BookSource) []BookSource {
	seen := map[string]bool{}
	out := make([]BookSource, 0, len(sources))
	for _, source := range sources {
		if shouldExcludeBookSource(source) {
			continue
		}
		source = cleanBookSourceName(source)
		key := sha1Hex(stringField(source, "bookSourceUrl") + "\x00" + stringField(source, "searchUrl") + "\x00" + stringField(source, "bookSourceName"))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, source)
	}
	return out
}

func effectiveSourceSpecs(cfg Config) []string {
	specs := append([]string{}, cfg.Sources...)
	if cfg.WatchDir != "" {
		for _, pattern := range []string{"*.json", "*.JSON"} {
			matches, _ := filepath.Glob(filepath.Join(cfg.WatchDir, pattern))
			specs = append(specs, matches...)
		}
	}
	sort.Strings(specs)
	return specs
}

func loadSourcesFromSpecs(specs []string, outputDir string) ([]BookSource, error) {
	all := []BookSource{}
	for _, spec := range specs {
		paths, err := materializeSourceSpec(spec, outputDir)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			sources, err := loadSources(path)
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", path, err)
			}
			all = append(all, sources...)
		}
	}
	return all, nil
}

func materializeSourceSpec(spec, outputDir string) ([]string, error) {
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") {
		return downloadSourceSpec(spec, outputDir)
	}
	if strings.Contains(spec, "::") || strings.HasSuffix(spec, ".git") || strings.HasPrefix(spec, "git@") {
		return cloneGitSourceSpec(spec, outputDir)
	}
	matches, err := filepath.Glob(spec)
	if err != nil {
		return nil, err
	}
	if len(matches) > 0 {
		return matches, nil
	}
	return []string{spec}, nil
}

func downloadSourceSpec(rawURL, outputDir string) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(outputDir, "sources"), 0755); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 booksource-tool")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", rawURL, resp.StatusCode)
	}
	path := filepath.Join(outputDir, "sources", sha1Hex(rawURL)+".json")
	out, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func cloneGitSourceSpec(spec, outputDir string) ([]string, error) {
	gitURL, pattern := spec, "**/*.json"
	if before, after, ok := strings.Cut(spec, "::"); ok {
		gitURL, pattern = before, after
	}
	cloneDir := filepath.Join(outputDir, "repos", sha1Hex(gitURL))
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil {
		cmd := exec.Command("git", "-C", cloneDir, "pull", "--ff-only")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git pull %s: %w: %s", gitURL, err, strings.TrimSpace(string(out)))
		}
	} else {
		cmd := exec.Command("git", "clone", "--depth", "1", gitURL, cloneDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git clone %s: %w: %s", gitURL, err, strings.TrimSpace(string(out)))
		}
	}
	matches, err := filepath.Glob(filepath.Join(cloneDir, pattern))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("git source %s matched no files with %s", gitURL, pattern)
	}
	return matches, nil
}

func publishFile(path, target string) error {
	cmd := exec.Command("scp", path, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("publish %s to %s: %w: %s", path, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func loadSources(path string) ([]BookSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sources []BookSource
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

func values(m map[string]ProbeResult) []ProbeResult {
	out := make([]ProbeResult, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProbeURL < out[j].ProbeURL })
	return out
}

func statusCounts(results map[string]ProbeResult) map[string]int {
	out := map[string]int{}
	for _, r := range results {
		out[fmt.Sprint(r.Status)]++
	}
	return out
}

func errorCounts(results map[string]ProbeResult) map[string]int {
	out := map[string]int{}
	for _, r := range results {
		if !r.OK && r.Error != "" {
			out[strings.SplitN(r.Error, ":", 2)[0]]++
		}
	}
	return out
}

func reloadConfig(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	updated := loadConfigFile("etc/config.json")
	if len(updated.SearchKeywords) == 0 {
		return false
	}
	old := cfg.SearchKeywords
	cfg.SearchKeywords = updated.SearchKeywords
	if strings.Join(old, ",") != strings.Join(cfg.SearchKeywords, ",") {
		logf("config reloaded: keywords %d -> %d\n", len(old), len(cfg.SearchKeywords))
		return true
	}
	return false
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
