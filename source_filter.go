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
	target := ProbeTargetFromBookSource(source)
	host := domainOf(target.URL)
	if isBlockedSearchEngineHost(host) {
		return true
	}
	searchURL := strings.ToLower(stringField(source, "searchUrl"))
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
