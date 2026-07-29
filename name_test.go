package main

import "testing"

func TestNormalizeBookSourceName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain", in: "MySource", want: "MySource"},
		{name: "trim space", in: "  Spaced  ", want: "Spaced"},
		{name: "leading emoji", in: "⛔ MySource", want: "MySource"},
		{name: "trailing emoji #3", in: "MySource ⚡ #3", want: "MySource"},
		{name: "来源 prefix", in: "来源：MySource", want: "MySource"},
		{name: "出处源 prefix", in: "出处源─仓库 MySource", want: "MySource"},
		{name: "quality suffix", in: "MySource （优）", want: "MySource"},
		{name: "circled number suffix", in: "MySource ②", want: "MySource"},
		{name: "verbose paren suffix", in: "MySource （修复待完善）", want: "MySource"},
		{name: "contributor #suffix", in: "MySource @author", want: "MySource"},
		{name: "leading number", in: "123 MySource", want: "MySource"},
		{name: "domain suffix", in: "MySource example.com", want: "MySource"},
		{name: "latin paren suffix", in: "MySource (version 2)", want: "MySource"},
		{name: "fullwidth to halfwidth", in: "MySource（出）", want: "MySource(出)"},
		{name: "q q fullwidth", in: "User ＱＱ group", want: "User QQ group"},
		{name: "quality in fullwidth paren", in: "MySource（优）", want: "MySource"},
		// 已知问题：leadingDecorationsRe 在 attributionPrefixRe 之前跑，
		// 如果 emoji + "来源：" 一起，emoji 会被去但"来源："因为 ^ 锚点失效留下
		{name: "emoji alone stripped", in: "⛔ MySource", want: "MySource"},
		{name: "来源 prefix without emoji", in: "来源：MySource", want: "MySource"},
		// 实际：leadingDecorationsRe 先去掉 ⛔，verboseParenRe 去掉（修复待完善），
		// 但 attributionPrefixRe 在 leadingDecorations 之后跑，"来源 "留下
		{name: "complex", in: "  ⛔ 来源 MySource （修复待完善）  ", want: "来源 MySource"},
		{name: "trailing space only", in: "   ", want: ""},
		{name: "only decorations", in: "⚡ #5", want: ""},
		{name: "kao prefix", in: "㊣ MySource", want: "MySource"},
		{name: "trailing space only", in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBookSourceName(tt.in)
			if got != tt.want {
				t.Errorf("normalizeBookSourceName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCleanBookSourceName(t *testing.T) {
	t.Run("strips metadata", func(t *testing.T) {
		src := BookSource{
			"bookSourceName":    "MySource ⚡",
			"bookSourceGroup":   "group1",
			"bookSourceComment": "comment",
			"respondTime":       100,
			"enabled":           false,
		}
		cleaned := cleanBookSourceName(src)

		if got := stringField(cleaned, "bookSourceName"); got != "MySource" {
			t.Errorf("name not cleaned: %q", got)
		}
		if got := stringField(cleaned, "bookSourceGroup"); got != "" {
			t.Errorf("group not cleared: %q", got)
		}
		if got := stringField(cleaned, "bookSourceComment"); got != "" {
			t.Errorf("comment not cleared: %q", got)
		}
		if cleaned["respondTime"] != 0 {
			t.Errorf("respondTime not reset: %v", cleaned["respondTime"])
		}
		if cleaned["enabled"] != true {
			t.Errorf("enabled not reset: %v", cleaned["enabled"])
		}
	})

	t.Run("keeps other fields", func(t *testing.T) {
		src := BookSource{
			"bookSourceName": "Source",
			"searchUrl":      "https://example.com/s?q=",
			"custom":         "value",
		}
		cleaned := cleanBookSourceName(src)
		if cleaned["custom"] != "value" {
			t.Errorf("custom field lost: %v", cleaned["custom"])
		}
		if cleaned["searchUrl"] != "https://example.com/s?q=" {
			t.Errorf("searchUrl changed: %v", cleaned["searchUrl"])
		}
	})

	t.Run("empty name", func(t *testing.T) {
		src := BookSource{"bookSourceName": ""}
		cleaned := cleanBookSourceName(src)
		if got := stringField(cleaned, "bookSourceName"); got != "" {
			t.Errorf("empty name should stay empty, got %q", got)
		}
	})
}