package main

import "testing"

func TestShouldExcludeBookSource(t *testing.T) {
	tests := []struct {
		name    string
		source  BookSource
		want    bool
		desc    string
	}{
		{
			name: "plain non-aggregator",
			source: BookSource{
				"bookSourceName": "MySource",
				"bookSourceUrl":  "https://example.com",
				"searchUrl":      "https://example.com/search?q={{key}}",
			},
			want: false,
			desc: "正常的源不应该被排除",
		},
		{
			name: "aggregator by name",
			source: BookSource{
				"bookSourceName": "聚合搜书",
				"bookSourceUrl":  "https://example.com",
				"searchUrl":      "https://example.com/search?q={{key}}",
			},
			want: true,
			desc: "聚合搜书应该被排除",
		},
		{
			name: "da hui lang aggregator",
			source: BookSource{
				"bookSourceName": "大灰狼书源",
				"bookSourceUrl":  "https://example.com",
				"searchUrl":      "https://example.com/s",
			},
			want: true,
		},
		{
			name: "qing tian aggregator",
			source: BookSource{
				"bookSourceName": "晴天书库",
				"bookSourceUrl":  "https://example.com",
				"searchUrl":      "https://example.com/s",
			},
			want: true,
		},
		{
			name: "blocked by port 5006",
			source: BookSource{
				"bookSourceName": "Some",
				"bookSourceUrl":  "http://example.com:5006/api",
				"searchUrl":      "/search",
			},
			want: true,
		},
		{
			name: "blocked by port 5008",
			source: BookSource{
				"bookSourceName": "Some",
				"bookSourceUrl":  "http://example.com:5008/api",
				"searchUrl":      "/search",
			},
			want: true,
		},
		{
			name: "JS placeholder URL",
			source: BookSource{
				"bookSourceName": "JS Source",
				"bookSourceUrl":  "https://example.com/%3Cjs%3E",
				"searchUrl":      "/search?q=",
			},
			want: true,
		},
		{
			name: "blocked search engine - bing",
			source: BookSource{
				"bookSourceName": "Bing Wrap",
				"bookSourceUrl":  "https://cn.bing.com",
				"searchUrl":      "/search?q={{key}}",
			},
			want: true,
		},
		{
			name: "blocked search engine - google",
			source: BookSource{
				"bookSourceName": "Google Wrap",
				"bookSourceUrl":  "https://google.com",
				"searchUrl":      "/search?q={{key}}",
			},
			want: true,
		},
		{
			name: "site: query on bing",
			source: BookSource{
				"bookSourceName": "Site Query",
				"bookSourceUrl":  "https://example.com",
				"searchUrl":      "https://bing.com/search?q=site:{{key}}",
			},
			want: true,
		},
		{
			name: "site: query on custom site",
			source: BookSource{
				"bookSourceName": "Site Custom",
				"bookSourceUrl":  "https://example.com",
				"searchUrl":      "https://example.com/search?q=site:{{key}}",
			},
			want: false,
			desc: "site: 在自定义站点不该被排除",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExcludeBookSource(tt.source)
			if got != tt.want {
				t.Errorf("shouldExcludeBookSource() = %v, want %v (%s)", got, tt.want, tt.desc)
			}
		})
	}
}

func TestIsBlockedSearchEngineHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"bing.com", true},
		{"www.bing.com", true},
		{"cn.bing.com", true},
		{"google.com", true},
		{"www.google.com", true},
		{"baidu.com", true},
		{"www.baidu.com", true},
		{"m.baidu.com", true},
		{"sogou.com", true},
		{"so.com", true},
		{"sm.cn", true},
		{"yz.m.sm.cn", true},

		// sub 子域名应该也算
		// 如果不是被完全 block 的 host，应该返回 false
		{"example.com", false},
		{"notbing.com", false},
		{"bing.com.evil.com", false}, // 后缀匹配，bing 是后缀
		{"", false},
		{" Bing.COM ", true}, // trim + lowercase
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := isBlockedSearchEngineHost(tt.host)
			if got != tt.want {
				t.Errorf("isBlockedSearchEngineHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}