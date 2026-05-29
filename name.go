package main

import (
	"regexp"
	"strings"
)

var (
	leadingDecorationsRe  = regexp.MustCompile(`^[\p{So}\p{Sk}\p{Mn}]+\s*`)
	trailingDecorationsRe = regexp.MustCompile(`[\p{So}\p{Sk}\p{Mn}]+\s*(#\d+)?$`)
	attributionPrefixRe   = regexp.MustCompile(`^(出处源[─\-]仓库\s*|来源[：:]\s*|仓库\s*)`)
	qualitySuffixRe       = regexp.MustCompile(`\s*[（(]\s*优[\s+＋]*[)）]\s*$`)
	contributorSuffixRe   = regexp.MustCompile(`\s*[#＃◎@].*$`)
	circledNumberSuffixRe = regexp.MustCompile(`[①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳]+\s*$`)
	domainSuffixRe        = regexp.MustCompile(`\s+[A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z]{2,}(/[^\s]*)?\s*$`)
	latinParenSuffixRe    = regexp.MustCompile(`\s*[（(][A-Za-z0-9][A-Za-z0-9 ._-]*[)）]\s*$`)
	leadingNumberRe       = regexp.MustCompile(`^\d+\s*`)
	verboseParenRe        = regexp.MustCompile(`\s*[（(](修复[^)）]*|部分可看|详情优化版|[•·].*)[)）]\s*$`)
	whitespaceRe          = regexp.MustCompile(`\s+`)
)

func cleanBookSourceName(source BookSource) BookSource {
	cleaned := make(BookSource, len(source))
	for key, value := range source {
		cleaned[key] = value
	}
	cleaned["bookSourceGroup"] = ""
	cleaned["bookSourceComment"] = ""
	cleaned["respondTime"] = 0
	name := normalizeBookSourceName(stringField(source, "bookSourceName"))
	if name != "" {
		cleaned["bookSourceName"] = name
	}
	return cleaned
}

func normalizeBookSourceName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "㊣")
	name = attributionPrefixRe.ReplaceAllString(name, "")
	name = leadingDecorationsRe.ReplaceAllString(name, "")
	name = trailingDecorationsRe.ReplaceAllString(name, "")
	name = contributorSuffixRe.ReplaceAllString(name, "")
	name = circledNumberSuffixRe.ReplaceAllString(name, "")
	name = leadingNumberRe.ReplaceAllString(name, "")
	name = qualitySuffixRe.ReplaceAllString(name, "")
	name = verboseParenRe.ReplaceAllString(name, "")
	name = domainSuffixRe.ReplaceAllString(name, "")
	name = latinParenSuffixRe.ReplaceAllString(name, "")
	name = strings.ReplaceAll(name, "ＱＱ", "QQ")
	name = strings.ReplaceAll(name, "（", "(")
	name = strings.ReplaceAll(name, "）", ")")
	name = strings.ReplaceAll(name, "，", ",")
	name = whitespaceRe.ReplaceAllString(name, " ")
	return strings.TrimSpace(name)
}
