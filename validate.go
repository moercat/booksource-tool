package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

const maxProbeBodyBytes = 256 << 10

type SearchValidation struct {
	OK    bool
	Level int
	Grade string
	Notes []string
}

func validateSearchResponse(source BookSource, body []byte, ms int64) SearchValidation {
	text := strings.TrimSpace(string(body))
	validation := SearchValidation{Grade: "D"}
	if len(text) == 0 {
		validation.Notes = append(validation.Notes, "empty response body")
		return validation
	}
	if len(text) < 80 {
		validation.Notes = append(validation.Notes, "short response body")
	}
	if marker := blockedResponseMarker(text); marker != "" {
		validation.Notes = append(validation.Notes, "blocked response: "+marker)
		return validation
	}

	validation.OK = true
	validation.Level = 1
	validation.Grade = gradeForValidation(1, ms)
	validation.Notes = append(validation.Notes, "http 200 with credible body")

	ruleSearch := map[string]any{}
	if rule, ok := source["ruleSearch"].(map[string]any); ok {
		ruleSearch = rule
	}
	keyword := searchKeyword(source)
	if keyword != "" && keyword != "我" && strings.Contains(strings.ToLower(text), strings.ToLower(keyword)) {
		validation.Level = maxInt(validation.Level, 2)
		validation.Notes = append(validation.Notes, "response contains check keyword")
	}

	if jsonLooksValid(text) {
		validation.Level = maxInt(validation.Level, validateJSONSearchBody(text, ruleSearch, &validation))
	} else {
		validation.Level = maxInt(validation.Level, validateHTMLSearchBody(text, ruleSearch, &validation))
	}
	validation.Grade = gradeForValidation(validation.Level, ms)
	return validation
}

func blockedResponseMarker(text string) string {
	lower := strings.ToLower(text)
	markers := []string{"captcha", "cloudflare", "安全验证", "验证码", "访问频繁", "人机验证", "forbidden", "access denied", "too many requests"}
	for _, marker := range markers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return marker
		}
	}
	return ""
}

func jsonLooksValid(text string) bool {
	var value any
	return json.Unmarshal([]byte(text), &value) == nil
}

func validateJSONSearchBody(text string, ruleSearch map[string]any, validation *SearchValidation) int {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return 1
	}
	level := 1
	if hasNonEmptyJSONCollection(value) {
		level = 2
		validation.Notes = append(validation.Notes, "json contains non-empty collection")
	}
	if ruleMatchesJSON(value, stringField(ruleSearch, "bookList")) {
		level = 3
		validation.Notes = append(validation.Notes, "ruleSearch.bookList matches json")
	}
	if level >= 2 && (ruleMatchesJSON(value, stringField(ruleSearch, "name")) || ruleMatchesJSON(value, stringField(ruleSearch, "bookUrl"))) {
		level = 3
		validation.Notes = append(validation.Notes, "ruleSearch item field matches json")
	}
	return level
}

func validateHTMLSearchBody(text string, ruleSearch map[string]any, validation *SearchValidation) int {
	level := 1
	if strings.Count(strings.ToLower(text), "<a ") > 0 || strings.Count(strings.ToLower(text), "href=") > 0 {
		level = 2
		validation.Notes = append(validation.Notes, "html contains links")
	}
	if ruleMatchesHTML(text, stringField(ruleSearch, "bookList")) {
		level = 3
		validation.Notes = append(validation.Notes, "ruleSearch.bookList matches html")
	}
	if level >= 2 && (ruleMatchesHTML(text, stringField(ruleSearch, "name")) || ruleMatchesHTML(text, stringField(ruleSearch, "bookUrl"))) {
		level = 3
		validation.Notes = append(validation.Notes, "ruleSearch item field matches html")
	}
	return level
}

func hasNonEmptyJSONCollection(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) > 0
	case map[string]any:
		for _, child := range typed {
			if hasNonEmptyJSONCollection(child) {
				return true
			}
		}
	}
	return false
}

func ruleMatchesJSON(value any, rule string) bool {
	for _, part := range splitRuleAlternatives(rule) {
		if !strings.HasPrefix(part, "$") {
			continue
		}
		path := strings.TrimPrefix(part, "$")
		path = strings.Trim(path, ".")
		path = strings.ReplaceAll(path, "[*]", "")
		if path == "" && hasNonEmptyJSONCollection(value) {
			return true
		}
		if jsonPathExists(value, strings.Split(path, ".")) {
			return true
		}
	}
	return false
}

func jsonPathExists(value any, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	part := strings.TrimSpace(parts[0])
	if part == "" {
		return jsonPathExists(value, parts[1:])
	}
	switch typed := value.(type) {
	case map[string]any:
		child, ok := typed[part]
		return ok && jsonPathExists(child, parts[1:])
	case []any:
		for _, child := range typed {
			if jsonPathExists(child, parts) {
				return true
			}
		}
	}
	return false
}

func ruleMatchesHTML(text, rule string) bool {
	lower := strings.ToLower(text)
	for _, part := range splitRuleAlternatives(rule) {
		part = strings.TrimSpace(strings.Split(part, "@")[0])
		if part == "" || strings.HasPrefix(part, "$") || strings.HasPrefix(part, "@js") {
			continue
		}
		if strings.HasPrefix(part, ".") {
			className := regexp.QuoteMeta(strings.TrimPrefix(part, "."))
			if regexp.MustCompile(`class=["'][^"']*`+className+`[^"']*["']`).FindStringIndex(lower) != nil {
				return true
			}
			continue
		}
		if strings.HasPrefix(part, "#") {
			id := regexp.QuoteMeta(strings.TrimPrefix(part, "#"))
			if regexp.MustCompile(`id=["']`+id+`["']`).FindStringIndex(lower) != nil {
				return true
			}
			continue
		}
		tag := strings.Trim(part, " /")
		if tag != "" && regexp.MustCompile(`<`+regexp.QuoteMeta(strings.ToLower(tag))+`[\s>]`).FindStringIndex(lower) != nil {
			return true
		}
	}
	return false
}

func splitRuleAlternatives(rule string) []string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return nil
	}
	replacer := strings.NewReplacer("||", "\n", "&&", "\n")
	rule = replacer.Replace(rule)
	return strings.Split(rule, "\n")
}

func gradeForValidation(level int, ms int64) string {
	switch {
	case level >= 3 && ms > 0 && ms < 1500:
		return "S"
	case level >= 3:
		return "A"
	case level == 2:
		return "B"
	case level == 1:
		return "C"
	default:
		return "D"
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
