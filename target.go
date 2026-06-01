package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

var defaultSearchKeywords = builtinDefaultSearchKeywords()

func builtinDefaultSearchKeywords() []string {
	return []string{"我", "修仙", "系统", "穿越", "总裁", "悬疑", "武侠", "斗破苍穹"}
}

func setDefaultSearchKeywords(keywords []string) {
	cleaned := uniqueSearchKeywords(keywords)
	if len(cleaned) == 0 {
		cleaned = builtinDefaultSearchKeywords()
	}
	defaultSearchKeywords = cleaned
}

func ProbeTargetFromBookSource(source BookSource) ProbeTarget {
	return ProbeTargetFromBookSourceKeyword(source, searchKeyword(source))
}

func ProbeTargetFromBookSourceKeyword(source BookSource, keyword string) ProbeTarget {
	baseURL := stringField(source, "bookSourceUrl")
	if target := probeTargetFromSearchURL(stringField(source, "searchUrl"), baseURL, keyword, false); target.URL != "" {
		return target
	}
	return ProbeTarget{}
}

func StructuralProbeTargetFromBookSource(source BookSource) ProbeTarget {
	baseURL := stringField(source, "bookSourceUrl")
	if target := probeTargetFromSearchURL(stringField(source, "searchUrl"), baseURL, "__BOOKSOURCE_KEY__", true); target.URL != "" {
		return target
	}
	return ProbeTarget{}
}

func probeTargetFromSearchURL(rawSearchURL, rawBaseURL, keyword string, structural bool) ProbeTarget {
	value := strings.TrimSpace(rawSearchURL)
	if value == "" || strings.HasPrefix(value, "@js:") {
		return ProbeTarget{}
	}
	for _, sep := range []string{"\n", "\r", "##", "@js:"} {
		if i := strings.Index(value, sep); i >= 0 {
			value = value[:i]
		}
	}
	urlPart, optionsPart := splitSearchURLOptions(value)
	method, body, webView := parseSearchOptions(optionsPart, keyword, structural)
	if webView {
		return ProbeTarget{}
	}
	value = replaceSearchPlaceholders(strings.TrimSpace(urlPart), keyword, structural)
	if value == "" || strings.HasPrefix(value, "data:") {
		return ProbeTarget{}
	}
	if hasUnresolvedTemplate(value) || hasUnresolvedTemplate(body) {
		return ProbeTarget{}
	}

	base := probeURLFromSourceURL(rawBaseURL)
	probeURL := ""
	if schemeRe.MatchString(value) {
		probeURL = cleanProbeURL(value)
	} else if strings.HasPrefix(value, "//") {
		probeURL = cleanProbeURL("https:" + value)
	} else if base == "" {
		if strings.Contains(value, ".") && !strings.ContainsAny(value, " /\t") {
			probeURL = cleanProbeURL("https://" + value)
		} else {
			return ProbeTarget{}
		}
	} else {
		baseParsed, err := url.Parse(base)
		if err != nil {
			return ProbeTarget{}
		}
		rel, err := url.Parse(value)
		if err != nil {
			return ProbeTarget{}
		}
		probeURL = cleanProbeURL(baseParsed.ResolveReference(rel).String())
	}
	if probeURL == "" {
		return ProbeTarget{}
	}
	return ProbeTarget{URL: probeURL, Method: method, Body: body}
}

func splitSearchURLOptions(value string) (string, string) {
	for i, r := range value {
		if r != ',' {
			continue
		}
		j := i + 1
		for j < len(value) && (value[j] == ' ' || value[j] == '\t' || value[j] == '\n' || value[j] == '\r') {
			j++
		}
		if j < len(value) && value[j] == '{' {
			return strings.TrimSpace(value[:i]), strings.TrimSpace(value[j:])
		}
	}
	return strings.TrimSpace(value), ""
}

func parseSearchOptions(optionsPart, keyword string, structural bool) (string, string, bool) {
	method := http.MethodGet
	if optionsPart == "" {
		return method, "", false
	}
	var optionsMap map[string]any
	if err := json.Unmarshal([]byte(optionsPart), &optionsMap); err != nil {
		return method, "", false
	}
	if webView, ok := optionsMap["webView"].(bool); ok && webView {
		return method, "", true
	}
	if value := stringField(optionsMap, "method"); value != "" {
		method = strings.ToUpper(value)
	}
	body := replaceSearchPlaceholders(stringField(optionsMap, "body"), keyword, structural)
	return method, body, false
}

func NormalizeProbeTarget(target ProbeTarget) string {
	if target.URL == "" {
		return ""
	}
	method := strings.ToUpper(strings.TrimSpace(target.Method))
	if method == "" {
		method = http.MethodGet
	}
	parsed, err := url.Parse(target.URL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	queryNames := make([]string, 0, len(parsed.Query()))
	for name := range parsed.Query() {
		queryNames = append(queryNames, strings.ToLower(name))
	}
	sort.Strings(queryNames)
	bodyNames := bodyParamNames(target.Body)
	base := method + " " + strings.TrimRight(parsed.Scheme+"://"+parsed.Host+parsed.EscapedPath(), "/")
	if len(queryNames) > 0 {
		base += " ?" + strings.Join(queryNames, "&")
	}
	if len(bodyNames) > 0 {
		base += " body:" + strings.Join(bodyNames, "&")
	}
	return base
}

func bodyParamNames(body string) []string {
	values, err := url.ParseQuery(body)
	if err != nil || len(values) == 0 {
		body = strings.TrimSpace(body)
		if body == "" {
			return nil
		}
		return []string{strings.ToLower(body)}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	return names
}

func replaceSearchPlaceholders(value, keyword string, structural bool) string {
	if strings.TrimSpace(keyword) == "" {
		keyword = "我"
	}
	encodedKeyword := keyword
	if !structural {
		encodedKeyword = url.QueryEscape(keyword)
	}
	replacements := []struct{ old, new string }{
		{"{{key}}", encodedKeyword},
		{"{{keyword}}", encodedKeyword},
		{"{{page}}", "1"},
		{"{{page-1}}", "0"},
		{"{{page+1}}", "2"},
		{"{key}", encodedKeyword},
		{"{keyword}", encodedKeyword},
		{"{page}", "1"},
		{"{page-1}", "0"},
		{"{page+1}", "2"},
	}
	for _, replacement := range replacements {
		value = strings.ReplaceAll(value, replacement.old, replacement.new)
	}
	value = regexp.MustCompile(`\{\{\s*encodeURIComponent\(\s*key\s*\)\s*\}\}`).ReplaceAllString(value, encodedKeyword)
	value = regexp.MustCompile(`\{\{\s*java\.encodeURI\(\s*key\s*,\s*['"][^'"]+['"]\s*\)\s*\}\}`).ReplaceAllString(value, encodedKeyword)
	value = regexp.MustCompile(`\{\{\s*\(?\s*page\s*-\s*1\s*\)?\s*\*\s*\d+\s*\}\}`).ReplaceAllString(value, "0")
	value = regexp.MustCompile(`\{\{\s*\(?\s*page\s*-\s*1\s*\)?\s*\}\}`).ReplaceAllString(value, "0")
	return value
}

func hasUnresolvedTemplate(value string) bool {
	return strings.Contains(value, "{{") || strings.Contains(value, "}}")
}

func searchKeyword(source BookSource) string {
	keywords := searchKeywords(source)
	if len(keywords) > 0 {
		return keywords[0]
	}
	return "我"
}

func searchKeywords(source BookSource) []string {
	keywords := []string{}
	if rule, ok := source["ruleSearch"].(map[string]any); ok {
		if keyword := stringField(rule, "checkKeyWord"); keyword != "" {
			keywords = append(keywords, keyword)
		}
	}
	keywords = append(keywords, defaultSearchKeywords...)
	return uniqueSearchKeywords(keywords)
}

func uniqueSearchKeywords(keywords []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" || seen[keyword] {
			continue
		}
		seen[keyword] = true
		out = append(out, keyword)
	}
	return out
}

func normalizeSourceURL(raw string) string {
	value := stripURLDecorations(raw)
	return strings.ToLower(value)
}

func cleanProbeURL(raw string) string {
	value := stripURLDecorations(raw)
	if value == "" || strings.HasPrefix(value, "data:") {
		return ""
	}
	if !schemeRe.MatchString(value) {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

func stripURLDecorations(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	for _, sep := range []string{",{", "##", "#", "@js:"} {
		if i := strings.Index(value, sep); i >= 0 {
			value = value[:i]
		}
	}
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func probeURLFromSourceURL(raw string) string {
	return cleanProbeURL(raw)
}

func domainOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
