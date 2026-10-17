package jsonparse

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FindScriptJSON extracts JSON objects from script tags matching the selector.
// If selector is empty, searches all script tags.
func FindScriptJSON(doc *goquery.Document, selector string) []map[string]any {
	var results []map[string]any

	sel := "script"
	if selector != "" {
		sel = selector
	}

	doc.Find(sel).Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text == "" {
			return
		}

		// Try direct JSON parse
		var data map[string]any
		if err := json.Unmarshal([]byte(text), &data); err == nil {
			results = append(results, data)
		}
	})

	return results
}

// FindScriptJSONByID extracts JSON from a script tag with the given ID
func FindScriptJSONByID(doc *goquery.Document, id string) (map[string]any, bool) {
	sel := doc.Find("script#" + id)
	if sel.Length() == 0 {
		return nil, false
	}

	text := strings.TrimSpace(sel.First().Text())
	if text == "" {
		return nil, false
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil, false
	}

	return data, true
}

// FindJSONInScripts searches all script tags for JSON containing a marker string
func FindJSONInScripts(doc *goquery.Document, marker string) []map[string]any {
	var results []map[string]any

	// Pattern to find JSON objects in script content
	re := regexp.MustCompile(`\{[^{}]*"` + regexp.QuoteMeta(marker) + `"[^{}]*\}|\{[\s\S]*?"` + regexp.QuoteMeta(marker) + `"[\s\S]*?\}`)

	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if !strings.Contains(text, marker) {
			return
		}

		// Try to find JSON assignment pattern: var X = {...}; or window.X = {...};
		patterns := []string{
			`=\s*(\{[\s\S]*\})\s*;?\s*$`,
			`=\s*(\{[\s\S]*\})\s*;`,
		}

		for _, p := range patterns {
			pRe := regexp.MustCompile(p)
			matches := pRe.FindStringSubmatch(text)
			if len(matches) > 1 {
				var data map[string]any
				if err := json.Unmarshal([]byte(matches[1]), &data); err == nil {
					results = append(results, data)
					return
				}
			}
		}

		// Try direct parse if whole content is JSON
		var data map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &data); err == nil {
			results = append(results, data)
		}

		// Last resort: find JSON-like substrings
		_ = re // Marker for potential future use
	})

	return results
}

// FindByTypename recursively finds all objects with matching __typename values
func FindByTypename(data any, typenames []string, maxDepth int) []map[string]any {
	return findByTypenameRecursive(data, typenames, 0, maxDepth)
}

func findByTypenameRecursive(data any, typenames []string, depth, maxDepth int) []map[string]any {
	if depth > maxDepth {
		return nil
	}

	var results []map[string]any

	switch v := data.(type) {
	case map[string]any:
		// Check if this object has a matching __typename
		if tn, ok := v["__typename"].(string); ok {
			for _, t := range typenames {
				if tn == t {
					results = append(results, v)
					break
				}
			}
		}
		// Recurse into all values
		for _, val := range v {
			results = append(results, findByTypenameRecursive(val, typenames, depth+1, maxDepth)...)
		}

	case []any:
		for _, item := range v {
			results = append(results, findByTypenameRecursive(item, typenames, depth+1, maxDepth)...)
		}
	}

	return results
}

// FindByKey recursively finds all objects containing a specific key
func FindByKey(data any, key string, maxDepth int) []map[string]any {
	return findByKeyRecursive(data, key, 0, maxDepth)
}

func findByKeyRecursive(data any, key string, depth, maxDepth int) []map[string]any {
	if depth > maxDepth {
		return nil
	}

	var results []map[string]any

	switch v := data.(type) {
	case map[string]any:
		if _, ok := v[key]; ok {
			results = append(results, v)
		}
		for _, val := range v {
			results = append(results, findByKeyRecursive(val, key, depth+1, maxDepth)...)
		}

	case []any:
		for _, item := range v {
			results = append(results, findByKeyRecursive(item, key, depth+1, maxDepth)...)
		}
	}

	return results
}

// GetString safely gets a nested string value
// Example: GetString(obj, "author", "username") returns obj["author"]["username"] as string
func GetString(obj map[string]any, path ...string) string {
	val := getNestedValue(obj, path)
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// GetBool safely gets a nested boolean value
func GetBool(obj map[string]any, path ...string) bool {
	val := getNestedValue(obj, path)
	switch v := val.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		return err == nil && b
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

// GetInt safely gets a nested int value
func GetInt(obj map[string]any, path ...string) int {
	val := getNestedValue(obj, path)
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

// GetInt64 safely gets a nested int64 value
func GetInt64(obj map[string]any, path ...string) int64 {
	val := getNestedValue(obj, path)
	switch v := val.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

// GetTime parses a unix timestamp (seconds or milliseconds) or date string
func GetTime(obj map[string]any, path ...string) time.Time {
	val := getNestedValue(obj, path)
	switch v := val.(type) {
	case int:
		return unixToTime(int64(v))
	case int64:
		return unixToTime(v)
	case float64:
		return unixToTime(int64(v))
	case string:
		// Try Twitter date format: "Mon Jan 02 15:04:05 +0000 2006"
		if t, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", v); err == nil {
			return t.UTC()
		}
		// Try ISO 8601
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC()
		}
		// Try unix string
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			return unixToTime(ts)
		}
	}
	return time.Time{}
}

// GetSlice safely gets a nested slice value
func GetSlice(obj map[string]any, path ...string) []any {
	val := getNestedValue(obj, path)
	if s, ok := val.([]any); ok {
		return s
	}
	return nil
}

// GetMap safely gets a nested map value
func GetMap(obj map[string]any, path ...string) map[string]any {
	val := getNestedValue(obj, path)
	if m, ok := val.(map[string]any); ok {
		return m
	}
	return nil
}

func getNestedValue(obj map[string]any, path []string) any {
	if len(path) == 0 {
		return nil
	}

	current := any(obj)
	for _, key := range path {
		switch v := current.(type) {
		case map[string]any:
			current = v[key]
		default:
			return nil
		}
	}
	return current
}

func unixToTime(ts int64) time.Time {
	// Detect milliseconds vs seconds
	if ts > 1e12 {
		return time.UnixMilli(ts).UTC()
	}
	return time.Unix(ts, 0).UTC()
}
