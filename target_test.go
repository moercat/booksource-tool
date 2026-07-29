package main

import "testing"

func TestNormalizeProbeTarget(t *testing.T) {
	tests := []struct {
		name   string
		target ProbeTarget
		want   string
	}{
		{
			name:   "empty target returns empty",
			target: ProbeTarget{},
			want:   "",
		},
		{
			name: "simple GET no query",
			target: ProbeTarget{
				URL:    "https://Example.com/path/",
				Method: "GET",
			},
			want: "GET https://example.com/path",
		},
		{
			name: "POST with body",
			target: ProbeTarget{
				URL:    "https://example.com/search",
				Method: "POST",
				Body:   "keyword=foo",
			},
			want: "POST https://example.com/search body:keyword",
		},
		{
			name: "GET with query (sorted)",
			target: ProbeTarget{
				URL:    "https://example.com/s?z=1&a=2",
				Method: "get",
			},
			want: "GET https://example.com/s ?a&z",
		},
		{
			name: "lowercase scheme + host",
			target: ProbeTarget{
				URL:    "HTTPS://EXAMPLE.COM/Search",
				Method: "get",
			},
			want: "GET https://example.com/Search",
		},
		{
			name: "default GET when method empty",
			target: ProbeTarget{
				URL: "https://example.com/search",
			},
			want: "GET https://example.com/search",
		},
		{
			name: "invalid URL returns empty",
			target: ProbeTarget{
				URL:    "not-a-url",
				Method: "GET",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeProbeTarget(tt.target)
			if got != tt.want {
				t.Errorf("NormalizeProbeTarget(%+v) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestReplaceSearchPlaceholders(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		keyword   string
		structural bool
		want      string
	}{
		{
			name:    "double brace {{key}}",
			value:   "/search?q={{key}}",
			keyword: "我",
			want:    "/search?q=%E6%88%91",
		},
		{
			name:    "double brace {{keyword}}",
			value:   "/search?q={{keyword}}",
			keyword: "修仙",
			want:    "/search?q=%E4%BF%AE%E4%BB%99",
		},
		{
			name:    "single brace {key}",
			value:   "/search?q={key}",
			keyword: "test",
			want:    "/search?q=test",
		},
		{
			name:    "page placeholders",
			value:   "/s?page={{page}}&prev={{page-1}}&next={{page+1}}",
			keyword: "k",
			want:    "/s?page=1&prev=0&next=2",
		},
		{
			name:    "structural mode no encoding",
			value:   "/search?q={{key}}",
			keyword: "我",
			structural: true,
			want:    "/search?q=我",
		},
		{
			name:    "encodeURIComponent",
			value:   "/s?q={{encodeURIComponent(key)}}",
			keyword: "Hello World",
			want:    "/s?q=Hello+World",
		},
		{
			name:    "no keyword defaults to 我",
			value:   "/s?q={{key}}",
			keyword: "",
			want:    "/s?q=%E6%88%91",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceSearchPlaceholders(tt.value, tt.keyword, tt.structural)
			if got != tt.want {
				t.Errorf("replaceSearchPlaceholders(%q, %q, %v) = %q, want %q",
					tt.value, tt.keyword, tt.structural, got, tt.want)
			}
		})
	}
}

func TestHasUnresolvedTemplate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "no template", value: "https://example.com", want: false},
		{name: "plain query", value: "https://example.com?q=foo", want: false},
		{name: "double brace unresolved", value: "{{key}}", want: true},
		{name: "double close", value: "foo}}", want: true},
		{name: "double open", value: "foo{{", want: true},
		// 注：单 {} 不算未解析（hasUnresolvedTemplate 只看 {{ }}）
		{name: "single brace", value: "{key}", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasUnresolvedTemplate(tt.value); got != tt.want {
				t.Errorf("hasUnresolvedTemplate(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestCleanProbeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// cleanProbeURL 不 lowercase host（这层在 NormalizeProbeTarget 做）
		{name: "valid https preserves host case", raw: "https://Example.com/path", want: "https://Example.com/path"},
		{name: "trailing slash stripped", raw: "https://example.com/path/", want: "https://example.com/path"},
		{name: "no scheme", raw: "example.com/path", want: "https://example.com/path"},
		{name: "strips fragment", raw: "https://example.com/path#frag", want: "https://example.com/path"},
		{name: "strips options", raw: "https://example.com/path,{}", want: "https://example.com/path"},
		{name: "strips js", raw: "https://example.com/path@js:{}", want: "https://example.com/path"},
		{name: "data url returns empty", raw: "data:text/html,foo", want: ""},
		{name: "empty", raw: "", want: ""},
		{name: "whitespace", raw: "  ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanProbeURL(tt.raw)
			if got != tt.want {
				t.Errorf("cleanProbeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDomainOf(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://Example.com/path", "example.com"},
		{"https://example.com:8080/path", "example.com"},
		{"not-a-url", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := domainOf(tt.raw)
			if got != tt.want {
				t.Errorf("domainOf(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestUniqueSearchKeywords(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: []string{}},
		{name: "no dup", in: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "with dup", in: []string{"a", "b", "a", "c", "b"}, want: []string{"a", "b", "c"}},
		{name: "empty entries trimmed", in: []string{"", "a", "", "b", " "}, want: []string{"a", "b"}},
		{name: "preserves order", in: []string{"c", "b", "a"}, want: []string{"c", "b", "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueSearchKeywords(tt.in)
			if tt.want == nil && got == nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}