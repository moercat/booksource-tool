package main

import "strings"

var blockedSearchEngineHosts = []string{
	"bing.com",
	"cn.bing.com",
	"www.bing.com",
	"google.com",
	"www.google.com",
	"baidu.com",
	"www.baidu.com",
	"m.baidu.com",
	"sogou.com",
	"www.sogou.com",
	"so.com",
	"www.so.com",
	"sm.cn",
	"yz.m.sm.cn",
}

func shouldExcludeBookSource(source BookSource) bool {
	name := stringField(source, "bookSourceName")
	searchURL := strings.ToLower(stringField(source, "searchUrl"))
	bookURL := stringField(source, "bookSourceUrl")

	// Exclude aggregate/meta-search sources
	if containsAny(name, []string{"聚合", "融合", "大灰狼", "晴天", "书山", "万能精品", "全本小说", "Icu", "聚合搜书", "聚合书库", "光遇接口"}) {
		return true
	}
	if containsAny(bookURL, []string{":5006", ":5008", ":45554"}) {
		return true
	}
	// Exclude sources with JS placeholder URLs (encoded <js> in the URL path)
	if strings.Contains(bookURL, "%3Cjs%3E") {
		return true
	}

	target := ProbeTargetFromBookSource(source)
	host := domainOf(target.URL)
	if isBlockedSearchEngineHost(host) {
		return true
	}
	return strings.Contains(searchURL, "site:") && containsAny(searchURL, []string{"bing.com/search", "baidu.com/s", "google.com/search", "sogou.com/web", "so.com/s", "sm.cn/s"})
}

func isBlockedSearchEngineHost(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	for _, blocked := range blockedSearchEngineHosts {
		blocked = strings.TrimPrefix(strings.ToLower(blocked), "www.")
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}
