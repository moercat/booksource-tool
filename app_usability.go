package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const appUsabilityValidationVersion = "2026-05-29.search-risk-v1"

func buildAppUsabilityReport(items []AppUsabilityReportItem) AppUsabilityReport {
	sort.Slice(items, func(i, j int) bool {
		if items[i].RiskScore != items[j].RiskScore {
			return items[i].RiskScore > items[j].RiskScore
		}
		left := items[i].SourceName + items[i].BookSourceURL
		right := items[j].SourceName + items[j].BookSourceURL
		return left < right
	})
	for i := range items {
		items[i].Index = i
	}
	report := AppUsabilityReport{
		GeneratedAt:       time.Now().Unix(),
		ValidationVersion: appUsabilityValidationVersion,
		ValidationScope:   "staged validation: primary search first, multi-keyword quality sampling only after primary search passes; source-provided metadata fields are omitted and ignored; does not execute Android WebView or Legado JS runtime",
		Count:             len(items),
		TierCounts:        map[string]int{},
		RiskCounts:        map[string]int{},
		ValidationGrades:  map[string]int{},
		Items:             items,
	}
	for _, item := range items {
		report.TierCounts[item.AppUsabilityTier]++
		grade := item.ValidationGrade
		if grade == "" {
			grade = "unknown"
		}
		report.ValidationGrades[grade]++
		for _, tag := range item.RiskTags {
			report.RiskCounts[tag]++
		}
	}
	return report
}

func appUsabilityItem(source BookSource, result ProbeResult, kept bool) AppUsabilityReportItem {
	risks := appUsabilityRiskTags(source, result)
	score := appUsabilityRiskScore(result)
	avgMS := successfulSampleAvgMS(result)
	section := analyzeSourceSections(source)
	tier := appUsabilityTier(score)
	return AppUsabilityReportItem{
		SourceName:            stringField(source, "bookSourceName"),
		BookSourceURL:         stringField(source, "bookSourceUrl"),
		SearchURL:             stringField(source, "searchUrl"),
		BookSourceType:        sourceTypeText(source),
		ValidationGrade:       result.ValidationGrade,
		ValidationLevel:       result.ValidationLevel,
		ValidationLevelText:   validationLevelText(result.ValidationLevel),
		ServerVerdict:         serverVerdict(score, risks),
		RuntimeRisk:           runtimeRisk(score),
		AppUsabilityTier:      tier,
		RiskScore:             score,
		RiskTags:              risks,
		ValidationStatusText:  validationStatusText(score, risks, result),
		ValidationGroup:       validationGroup(score, risks),
		SearchStatus:          searchStatusText(result),
		SearchMS:              result.MS,
		SampleTotal:           result.SampleTotal,
		SampleSuccesses:       result.SampleSuccesses,
		SampleKeywords:        result.SampleKeywords,
		SuccessfulSampleAvgMS: avgMS,
		SearchSpeedTier:       searchSpeedTier(avgMS),
		ProbeURL:              result.ProbeURL,
		BodyBytes:             result.BodyBytes,
		ValidationNotes:       result.ValidationNotes,
		DetailRisk:            section.DetailRisk,
		TocRisk:               section.TocRisk,
		ContentRisk:           section.ContentRisk,
		ExploreRisk:           section.ExploreRisk,
		RuntimeRequiredFor:    section.RuntimeRequiredFor,
		KeptInExport:          kept,
	}
}

type sourceSectionAnalysis struct {
	DetailRisk         string
	TocRisk            string
	ContentRisk        string
	ExploreRisk        string
	RuntimeRequiredFor []string
}

func analyzeSourceSections(source BookSource) sourceSectionAnalysis {
	analysis := sourceSectionAnalysis{
		DetailRisk:  sectionRisk(source, "ruleBookInfo", []string{"name"}),
		TocRisk:     sectionRisk(source, "ruleToc", []string{"chapterList"}),
		ContentRisk: sectionRisk(source, "ruleContent", []string{"content"}),
		ExploreRisk: exploreRisk(source),
	}
	for section, risk := range map[string]string{"detail": analysis.DetailRisk, "toc": analysis.TocRisk, "content": analysis.ContentRisk, "explore": analysis.ExploreRisk} {
		if risk == "js_runtime" || risk == "webview_runtime" || risk == "mixed_runtime" {
			analysis.RuntimeRequiredFor = append(analysis.RuntimeRequiredFor, section)
		}
	}
	return analysis
}

func sectionRisk(source BookSource, key string, required []string) string {
	section, ok := source[key].(map[string]any)
	if !ok || len(section) == 0 {
		return "missing"
	}
	for _, field := range required {
		if stringField(section, field) == "" {
			return "missing_" + field
		}
	}
	text := strings.ToLower(fmt.Sprint(section))
	if strings.Contains(text, "webview") || strings.Contains(text, "getwebviewua") || strings.Contains(text, "startbrowser") {
		return "webview_runtime"
	}
	if strings.Contains(text, "@js") || strings.Contains(text, "<js>") || strings.Contains(text, "java.") {
		return "js_runtime"
	}
	return "static_rule"
}

func exploreRisk(source BookSource) string {
	if enabled, ok := source["enabledExplore"].(bool); ok && !enabled {
		return "disabled"
	}
	exploreURL := stringField(source, "exploreUrl")
	ruleExplore, hasRule := source["ruleExplore"].(map[string]any)
	if exploreURL == "" && !hasRule {
		return "missing"
	}
	text := strings.ToLower(exploreURL + " " + fmt.Sprint(ruleExplore))
	if strings.Contains(text, "webview") || strings.Contains(text, "getwebviewua") || strings.Contains(text, "startbrowser") {
		return "webview_runtime"
	}
	if strings.Contains(text, "@js") || strings.Contains(text, "<js>") || strings.Contains(text, "java.") {
		return "js_runtime"
	}
	if strings.HasPrefix(strings.TrimSpace(exploreURL), "[") {
		return "json_explore"
	}
	if exploreURL != "" {
		return "static_url"
	}
	return "static_rule"
}

func appUsabilityRiskTags(source BookSource, result ProbeResult) []string {
	tags := []string{}
	all := strings.ToLower(fmt.Sprint(source))
	nameText := strings.ToLower(stringField(source, "bookSourceName"))
	bookSourceURL := strings.TrimSpace(stringField(source, "bookSourceUrl"))
	bookSourceType := sourceTypeText(source)

	if bookSourceType == "2" || containsAny(nameText, []string{"漫画", "漫蛙", "comic", "manga", "manhua"}) {
		tags = append(tags, "comic_source")
	}
	if bookSourceType == "1" || containsAny(nameText, []string{"听书", "有声", "audio"}) {
		tags = append(tags, "audio_source")
	}
	if strings.HasPrefix(bookSourceURL, "http://") {
		tags = append(tags, "http_only")
	}
	if strings.Contains(all, "@js") || strings.Contains(all, "<js>") {
		tags = append(tags, "js_rule")
	}
	if strings.Contains(all, "webview") || strings.Contains(all, "getwebviewua") || strings.Contains(all, "startbrowser") {
		tags = append(tags, "webview_rule")
	}
	if result.ValidationLevel > 0 && result.ValidationLevel < 2 || result.ValidationGrade == "C" || result.ValidationGrade == "D" {
		tags = append(tags, "weak_search_validation")
	}
	if result.MS >= 3000 {
		tags = append(tags, "slow_server_search")
	}
	if result.SampleTotal > 0 && result.SampleSuccesses == 0 {
		tags = append(tags, "multi_keyword_search_failed")
	}
	if result.SampleTotal >= 4 && result.SampleSuccesses > 0 && result.SampleSuccesses*2 < result.SampleTotal {
		tags = append(tags, "low_keyword_coverage")
	}
	if containsAny(all, []string{"captcha", "cloudflare", "人机", "验证码", "安全验证", "cf-chl"}) {
		tags = append(tags, "anti_bot_hint")
	}
	return uniqueStrings(tags)
}

func appUsabilityRiskScore(result ProbeResult) int {
	var score int
	if result.SampleTotal > 0 {
		score = result.SampleSuccesses * 100 / result.SampleTotal
	} else {
		switch {
		case result.ValidationLevel >= 3:
			score = 80
		case result.ValidationLevel == 2:
			score = 65
		case result.ValidationLevel == 1:
			score = 35
		default:
			score = 0
		}
	}
	score -= searchSpeedPenalty(successfulSampleAvgMS(result))
	if score < 0 {
		return 0
	}
	return score
}

func successfulSampleAvgMS(result ProbeResult) int64 {
	if len(result.SampleResults) == 0 {
		if result.OK {
			return result.MS
		}
		return 0
	}
	var total int64
	var count int64
	for _, sample := range result.SampleResults {
		if sample.OK && sample.MS > 0 {
			total += sample.MS
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / count
}

func searchSpeedPenalty(avgMS int64) int {
	switch {
	case avgMS <= 0:
		return 0
	case avgMS <= 1000:
		return 0
	case avgMS <= 5000:
		return 5
	default:
		return 10
	}
}

func searchSpeedTier(avgMS int64) string {
	switch {
	case avgMS <= 0:
		return "unknown"
	case avgMS <= 1000:
		return "fast_1s"
	case avgMS <= 5000:
		return "normal_5s"
	default:
		return "very_slow_over_5s"
	}
}

func appUsabilityTier(score int) string {
	switch {
	case score >= 70:
		return "likely_app_usable"
	case score >= 40:
		return "needs_app_check"
	default:
		return "high_risk"
	}
}

func serverVerdict(score int, risks []string) string {
	switch appUsabilityTier(score) {
	case "likely_app_usable":
		return "likely_usable"
	case "needs_app_check":
		return "partially_verified"
	default:
		return "high_risk"
	}
}

func runtimeRisk(score int) string {
	switch {
	case score >= 70:
		return "low"
	case score >= 40:
		return "medium"
	default:
		return "high"
	}
}

func validationStatusText(score int, risks []string, result ProbeResult) string {
	if result.SampleTotal > 0 {
		return fmt.Sprintf("多关键词搜索成功 %d/%d，成功样本平均 %dms", result.SampleSuccesses, result.SampleTotal, successfulSampleAvgMS(result))
	}
	if result.ValidationLevel >= 3 {
		return "服务端搜索强校验通过"
	}
	if result.ValidationLevel >= 2 {
		return "服务端搜索基础校验通过"
	}
	return "仅搜索可达，阅读链路未验证"
}

func validationGroup(score int, risks []string) string {
	if score >= 70 {
		return "关键词覆盖高"
	}
	if score >= 40 {
		return "关键词覆盖中"
	}
	return "关键词覆盖低"
}

func validationLevelText(level int) string {
	switch {
	case level >= 3:
		return "search_rule_match"
	case level == 2:
		return "search_body_structured"
	case level == 1:
		return "search_http_only"
	default:
		return "unknown"
	}
}

func searchStatusText(result ProbeResult) string {
	if result.OK {
		return "ok"
	}
	if result.Error != "" {
		return result.Error
	}
	return fmt.Sprintf("status_%d", result.Status)
}

func sourceTypeText(source BookSource) string {
	if v, ok := source["bookSourceType"]; ok {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func int64Field(source BookSource, key string) int64 {
	switch v := source[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case jsonNumberLike:
		return v.Int64()
	default:
		return 0
	}
}

type jsonNumberLike interface {
	Int64() int64
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
