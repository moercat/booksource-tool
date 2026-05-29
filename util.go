package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"sort"
	"strings"
)

func chooseRepresentative(items []BookSource) BookSource {
	sort.SliceStable(items, func(i, j int) bool { return quality(items[i]) > quality(items[j]) })
	return items[0]
}

func quality(source BookSource) int {
	q := 0
	if b, ok := source["enabled"].(bool); !ok || b {
		q++
	}
	if stringField(source, "searchUrl") != "" {
		q++
	}
	if rule, ok := source["ruleSearch"].(map[string]any); ok {
		if stringField(rule, "bookList") != "" {
			q++
		}
		if stringField(rule, "bookUrl") != "" {
			q++
		}
	}
	return q
}

func namesOf(items []BookSource) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, stringField(item, "bookSourceName"))
	}
	return out
}

func rawURLsOf(items []BookSource) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, stringField(item, "bookSourceUrl"))
	}
	return out
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func sha1Hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fileSHA1(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha1Hex(string(data)), nil
}
