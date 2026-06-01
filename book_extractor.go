package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// extractSearchBooks attempts to parse the search response body and extract
// book items using the source's ruleSearch rules. Returns extracted items
// with at minimum name and bookUrl populated.
func extractSearchBooks(body []byte, ruleSearch map[string]any, isJSON bool) []map[string]string {
	if isJSON {
		return extractJSONSearchBooks(body, ruleSearch)
	}
	return extractHTMLSearchBooks(body, ruleSearch)
}

func extractJSONSearchBooks(body []byte, ruleSearch map[string]any) []map[string]string {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	bookListRule := strings.TrimSpace(stringField(ruleSearch, "bookList"))
	if bookListRule == "" {
		return nil
	}
	items := resolveJSONPath(root, bookListRule)
	if len(items) == 0 {
		return nil
	}
	fields := []string{"name", "bookUrl", "author", "lastChapter", "kind", "wordCount", "coverUrl", "intro", "updateTime"}
	books := make([]map[string]string, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			book := map[string]string{}
			for _, field := range fields {
				rule := strings.TrimSpace(stringField(ruleSearch, field))
				if rule != "" {
					book[field] = resolveJSONString(m, rule)
				}
			}
			if book["name"] != "" || book["bookUrl"] != "" {
				books = append(books, book)
			}
		}
	}
	return books
}

func extractHTMLSearchBooks(body []byte, ruleSearch map[string]any) []map[string]string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	bookListRule := strings.TrimSpace(stringField(ruleSearch, "bookList"))
	if bookListRule == "" {
		return nil
	}
	sel := doc.Find(cssSelectorFromRule(bookListRule))
	if sel.Length() == 0 {
		return nil
	}
	fields := []string{"name", "bookUrl", "author", "lastChapter", "kind", "wordCount", "coverUrl", "intro", "updateTime"}
	books := make([]map[string]string, 0, sel.Length())
	sel.Each(func(i int, s *goquery.Selection) {
		book := map[string]string{}
		for _, field := range fields {
			rule := strings.TrimSpace(stringField(ruleSearch, field))
			if rule != "" {
				book[field] = extractHTMLField(s, rule)
			}
		}
		if book["name"] != "" || book["bookUrl"] != "" {
			books = append(books, book)
		}
	})
	return books
}

// resolveJSONPath resolves a simplified JSON path like "$.data.list" or "$.[*].name"
// against a parsed JSON value. Supports array wildcard [*].
func resolveJSONPath(root any, path string) []any {
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimSpace(path)
	if path == "" {
		return []any{root}
	}
	parts := strings.Split(path, ".")
	current := []any{root}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "[*]" {
			continue
		}
		// Handle array index like [0], [1]
		arrIdx := -1
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			fmt.Sscanf(part, "[%d]", &arrIdx)
			part = ""
		}
		next := []any{}
		for _, c := range current {
			switch v := c.(type) {
			case map[string]any:
				if part != "" {
					if child, ok := v[part]; ok {
						next = append(next, child)
					}
				} else if arrIdx >= 0 {
					// map with numeric key
					if child, ok := v[fmt.Sprintf("%d", arrIdx)]; ok {
						next = append(next, child)
					}
				}
			case []any:
				if part != "" {
					for _, item := range v {
						if m, ok := item.(map[string]any); ok {
							if child, ok := m[part]; ok {
								next = append(next, child)
							}
						}
					}
				} else if arrIdx >= 0 && arrIdx < len(v) {
					next = append(next, v[arrIdx])
				} else if arrIdx < 0 {
					// No index specified, expand array
					next = append(next, v...)
				}
			}
		}
		current = next
		if len(current) == 0 {
			return nil
		}
	}
	return current
}

func resolveJSONString(root any, path string) string {
	parts := resolveJSONPath(root, path)
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprint(parts[0])
}

// cssSelectorFromRule converts a Legado rule like ".book-item" or "#result a"
// to a standard CSS selector used by goquery. Most Legado CSS selectors are
// already standard CSS selectors.
func cssSelectorFromRule(rule string) string {
	if idx := strings.Index(rule, "@"); idx >= 0 {
		rule = rule[:idx]
	}
	// Strip Legado positional suffixes like .0, .1, .2 (not CSS class selectors like .classname)
	// A dot followed by digits immediately after non-whitespace is a Legado positional index.
	parts := strings.Fields(rule)
	for i, part := range parts {
		// Strip numeric-position suffixes: a.1 -> a,  div.3  -> div
		// But keep class selectors: .className, li.lastLine
		parts[i] = regexp.MustCompile(`([\w-]+)\.\d+\b`).ReplaceAllString(part, "$1")
		// Also strip colon-position suffixes: a:1 -> a
		parts[i] = regexp.MustCompile(`:\d+\b`).ReplaceAllString(parts[i], "")
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func extractHTMLField(sel *goquery.Selection, rule string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return ""
	}
	// Handle @attr suffix
	attr := ""
	if idx := strings.Index(rule, "@"); idx >= 0 {
		attr = rule[idx+1:]
		rule = rule[:idx]
	}
	// Handle :0, :1 positional suffixes
	posStr := ""
	if idx := strings.Index(rule, ":"); idx >= 0 && idx+1 < len(rule) {
		posStr = rule[idx+1:]
		rule = rule[:idx]
	}

	rule = strings.TrimSpace(rule)
	child := sel
	if rule != "" {
		child = sel.Find(cssSelectorFromRule(rule))
	}
	if child.Length() == 0 {
		return ""
	}

	idx := 0
	if posStr != "" {
		fmt.Sscanf(posStr, "%d", &idx)
	}
	if idx >= child.Length() {
		return ""
	}
	ch := child.Eq(idx)

	switch attr {
	case "href":
		return strings.TrimSpace(ch.AttrOr("href", ""))
	case "src":
		return strings.TrimSpace(ch.AttrOr("src", ""))
	case "text":
		return strings.TrimSpace(ch.Text())
	case "html":
		html, _ := ch.Html()
		return strings.TrimSpace(html)
	default:
		if attr != "" {
			return strings.TrimSpace(ch.AttrOr(attr, ""))
		}
		return strings.TrimSpace(ch.Text())
	}
}
