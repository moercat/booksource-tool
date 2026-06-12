package main

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const (
	SourceStatusNew      = "new"
	SourceStatusUsable   = "usable"
	SourceStatusAbnormal = "abnormal"
	SourceStatusDisabled = "disabled"
)

type PoolEntry struct {
	Key                 string          `json:"key" bson:"_id"`
	Source              BookSource      `json:"source" bson:"source"`
	ProbeTarget         ProbeTarget     `json:"probeTarget" bson:"probeTarget"`
	Status              string          `json:"status" bson:"status"`
	ProbeCount          int             `json:"probeCount" bson:"probeCount"`
	FailureCount        int             `json:"failureCount" bson:"failureCount"`
	ConsecutiveFailures int             `json:"consecutiveFailures" bson:"consecutiveFailures"`
	LastProbeAt         int64           `json:"lastProbeAt" bson:"lastProbeAt"`
	LastSuccessAt       int64           `json:"lastSuccessAt" bson:"lastSuccessAt"`
	LastFailureAt       int64           `json:"lastFailureAt" bson:"lastFailureAt"`
	AbnormalSince       int64           `json:"abnormalSince" bson:"abnormalSince"`
	NextProbeAt         int64           `json:"nextProbeAt" bson:"nextProbeAt"`
	DisabledAt          int64           `json:"disabledAt,omitempty" bson:"disabledAt,omitempty"`
	DisabledReason      string          `json:"disabledReason,omitempty" bson:"disabledReason,omitempty"`
	LastProbe           ProbeResult     `json:"lastProbe" bson:"lastProbe"`
	Origins             []string        `json:"origins" bson:"origins"`
	CreatedAt           int64           `json:"createdAt" bson:"createdAt"`
	UpdatedAt           int64           `json:"updatedAt" bson:"updatedAt"`
	Versions            []SourceVersion `json:"versions,omitempty" bson:"versions,omitempty"`
}

type SourcePool struct {
	Entries map[string]*PoolEntry
	Config  Config
}

func NewSourcePool(cfg Config) *SourcePool {
	return &SourcePool{Entries: map[string]*PoolEntry{}, Config: cfg}
}

func (pool *SourcePool) MergeSources(sources []BookSource, origin string, now time.Time) int {
	added := 0
	for _, source := range sources {
		if pool.AddOrUpdate(source, origin, now) {
			added++
		}
	}
	return added
}

func (pool *SourcePool) AddOrUpdate(source BookSource, origin string, now time.Time) bool {
	if shouldExcludeBookSource(source) {
		return false
	}
	source = cleanBookSourceName(source)
	structuralTarget := StructuralProbeTargetFromBookSource(source)
	probeTarget := ProbeTargetFromBookSource(source)
	key := NormalizeProbeTarget(structuralTarget)
	if key == "" || structuralTarget.URL == "" || probeTarget.URL == "" {
		return false
	}
	nowUnix := now.Unix()
	entry := pool.Entries[key]
	if entry == nil {
		pool.Entries[key] = &PoolEntry{Key: key, Source: source, ProbeTarget: probeTarget, Status: SourceStatusNew, NextProbeAt: nowUnix, Origins: []string{origin}, CreatedAt: nowUnix, UpdatedAt: nowUnix}
		return true
	}
	entry.Source = chooseRepresentative([]BookSource{entry.Source, source})
	entry.ProbeTarget = ProbeTargetFromBookSource(entry.Source)
	entry.Origins = appendUnique(entry.Origins, origin)
	entry.UpdatedAt = nowUnix
	entry.Versions = nil
	if entry.Status == SourceStatusDisabled {
		entry.Status = SourceStatusNew
		entry.DisabledAt = 0
		entry.DisabledReason = ""
		entry.ConsecutiveFailures = 0
		entry.NextProbeAt = nowUnix
	}
	return false
}

func (pool *SourcePool) DueJobs(now time.Time) map[string]ProbeJob {
	dueEntries := []*PoolEntry{}
	nowUnix := now.Unix()
	for _, entry := range pool.Entries {
		if entry.Status == SourceStatusDisabled || entry.NextProbeAt == 0 || entry.NextProbeAt > nowUnix {
			continue
		}
		dueEntries = append(dueEntries, entry)
	}
	sort.Slice(dueEntries, func(i, j int) bool {
		if dueEntries[i].NextProbeAt != dueEntries[j].NextProbeAt {
			return dueEntries[i].NextProbeAt < dueEntries[j].NextProbeAt
		}
		return dueEntries[i].Key < dueEntries[j].Key
	})
	limit := pool.Config.MaxProbeJobsPerCycle
	if limit <= 0 || limit > len(dueEntries) {
		limit = len(dueEntries)
	}
	due := map[string]ProbeJob{}
	for _, entry := range dueEntries[:limit] {
		due[entry.Key] = ProbeJob{Target: entry.ProbeTarget, Source: entry.Source, Previous: entry.LastProbe}
	}
	return due
}

func (pool *SourcePool) RecordProbe(key string, result ProbeResult, now time.Time) {
	entry := pool.Entries[key]
	if entry == nil {
		return
	}
	nowUnix := now.Unix()
	entry.ProbeCount++
	entry.LastProbe = compactProbeResult(result)
	entry.LastProbeAt = nowUnix
	entry.UpdatedAt = nowUnix
	if result.OK {
		entry.Status = SourceStatusUsable
		entry.ConsecutiveFailures = 0
		entry.AbnormalSince = 0
		entry.LastSuccessAt = nowUnix
		entry.DisabledAt = 0
		entry.DisabledReason = ""
		entry.NextProbeAt = now.Add(jitter(pool.Config.UsableProbeInterval, key)).Unix()
		return
	}
	entry.Status = SourceStatusAbnormal
	entry.FailureCount++
	entry.ConsecutiveFailures++
	entry.LastFailureAt = nowUnix
	if entry.AbnormalSince == 0 {
		entry.AbnormalSince = nowUnix
	}
	delay := pool.nextFailureDelay(entry, now)
	if delay <= 0 {
		entry.Status = SourceStatusDisabled
		entry.DisabledAt = nowUnix
		entry.DisabledReason = "abnormal too long"
		entry.NextProbeAt = 0
		return
	}
	entry.NextProbeAt = now.Add(jitter(delay, key)).Unix()
}

func (pool *SourcePool) nextFailureDelay(entry *PoolEntry, now time.Time) time.Duration {
	abnormalFor := time.Duration(0)
	if entry.AbnormalSince > 0 {
		abnormalFor = now.Sub(time.Unix(entry.AbnormalSince, 0))
	}
	if abnormalFor >= 90*24*time.Hour && entry.ConsecutiveFailures >= pool.Config.MaxConsecutiveFailures {
		return 0
	}
	if abnormalFor >= 30*24*time.Hour && entry.ConsecutiveFailures >= 30 {
		return 7 * 24 * time.Hour
	}
	if abnormalFor >= 7*24*time.Hour && entry.ConsecutiveFailures >= 7 {
		return 3 * 24 * time.Hour
	}
	return pool.Config.InvalidProbeInterval
}

func (pool *SourcePool) UsableSources() []BookSource {
	type sortEntry struct {
		source  BookSource
		success int
		avgMS   int64
		order   int // 0=normal, 1=comic, 2=audio
	}
	entries := []sortEntry{}
	for _, entry := range pool.Entries {
		if entry.Status == SourceStatusUsable && !shouldExcludeBookSource(entry.Source) {
			source := cleanBookSourceName(entry.Source)
			nameText := strings.ToLower(stringField(source, "bookSourceName"))
			srcType := stringField(source, "bookSourceType")
			order := 0
			if srcType == "2" || containsAny(nameText, []string{"漫画", "漫蛙", "comic", "manga", "manhua"}) {
				order = 1
			} else if srcType == "1" || containsAny(nameText, []string{"听书", "有声", "audio"}) {
				order = 2
			}
			avgMS := successfulSampleAvgMS(entry.LastProbe)
			entries = append(entries, sortEntry{
				source:  source,
				success: entry.LastProbe.SampleSuccesses,
				avgMS:   avgMS,
				order:   order,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].order != entries[j].order {
			return entries[i].order < entries[j].order
		}
		if entries[i].success != entries[j].success {
			return entries[i].success > entries[j].success
		}
		if entries[i].avgMS != entries[j].avgMS {
			if entries[i].avgMS == 0 {
				return false
			}
			if entries[j].avgMS == 0 {
				return true
			}
			return entries[i].avgMS < entries[j].avgMS
		}
		left := stringField(entries[i].source, "bookSourceName") + stringField(entries[i].source, "bookSourceUrl")
		right := stringField(entries[j].source, "bookSourceName") + stringField(entries[j].source, "bookSourceUrl")
		return left < right
	})
	usable := make([]BookSource, len(entries))
	for i, e := range entries {
		usable[i] = e.source
	}
	return usable
}

func (pool *SourcePool) Summary(outputHash string, outputs map[string]string) Summary {
	statusCounts := map[string]int{}
	errorCounts := map[string]int{}
	for _, entry := range pool.Entries {
		statusCounts[entry.Status]++
		if !entry.LastProbe.OK && entry.LastProbe.Error != "" {
			errorCounts[entry.LastProbe.Error]++
		}
	}
	usable := statusCounts[SourceStatusUsable]
	return Summary{Input: "source-pool", Records: len(pool.Entries), UniqueNormalizedURLs: len(pool.Entries), UsableDedupSources: usable, FailedUniqueURLs: len(pool.Entries) - usable, StatusCounts: statusCounts, ErrorCounts: errorCounts, Outputs: outputs, UpdatedAt: time.Now().Unix()}
}

func stableJSONHash(value any) (string, []byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return sha1Hex(string(data)), data, nil
}

func compactProbeResult(result ProbeResult) ProbeResult {
	result.rawBody = nil
	result.Body = ""
	result.SampleKeywords = nil
	result.SampleResults = nil
	result.SampleTotal = 0
	result.SampleSuccesses = 0
	result.ExtractedName = ""
	result.ExtractedAuthor = ""
	result.ExtractedLastChapter = ""
	result.ExtractedKind = ""
	result.ExtractedWords = ""
	result.DetailOK = false
	result.ContentOK = false
	if len(result.ValidationNotes) > 1 {
		result.ValidationNotes = result.ValidationNotes[:1]
	}
	return result
}

func appendUnique(values []string, value string) []string {
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

func jitter(base time.Duration, key string) time.Duration {
	if base <= 0 {
		return base
	}
	spread := int64(base / 10)
	if spread <= 0 {
		return base
	}
	return base + time.Duration(int64Hash(key)%(2*spread)-spread)
}

func int64Hash(value string) int64 {
	var out int64
	for _, r := range value {
		out = out*131 + int64(r)
		if out < 0 {
			out = -out
		}
	}
	return out
}
