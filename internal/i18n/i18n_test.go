package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	tmplKey   = regexp.MustCompile(`\{\{t "((?:[^"\\]|\\.)*)"`)
	jsKey     = regexp.MustCompile(`\bt\('((?:[^'\\]|\\.)*)'`)
	goKey     = regexp.MustCompile(`(?:\.tr|\.T|tr\(r\)|failf)\(\s*((?:"(?:[^"\\]|\\.)*"\s*\+?\s*)+)`)
	goStr     = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	formatVrb = regexp.MustCompile(`%[sdq]`)
)

// usedKeys collects translatable strings from templates, scripts and Go code.
func usedKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	add := func(k string) {
		if k = strings.ReplaceAll(k, `\"`, `"`); !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".min.js") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		switch filepath.Ext(p) {
		case ".html":
			for _, m := range tmplKey.FindAllStringSubmatch(src, -1) {
				add(m[1])
			}
		case ".js":
			for _, m := range jsKey.FindAllStringSubmatch(src, -1) {
				add(m[1])
			}
		case ".go":
			if strings.Contains(p, "i18n") {
				return nil
			}
			for _, m := range goKey.FindAllStringSubmatch(src, -1) {
				var full strings.Builder
				for _, s := range goStr.FindAllStringSubmatch(m[1], -1) {
					full.WriteString(s[1])
				}
				add(full.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestRussianCoversEveryKey(t *testing.T) {
	keys := usedKeys(t)
	if len(keys) < 100 {
		t.Fatalf("found only %d keys: the extractor is broken", len(keys))
	}
	for _, k := range keys {
		if _, ok := ru[k]; !ok {
			t.Errorf("missing ru translation: %q", k)
		}
	}
}

func TestFormatVerbsMatch(t *testing.T) {
	for k, v := range ru {
		if a, b := formatVrb.FindAllString(k, -1), formatVrb.FindAllString(v, -1); !slices.Equal(a, b) {
			t.Errorf("verbs differ: %q %v vs %q %v", k, a, v, b)
		}
	}
}

func TestT(t *testing.T) {
	if got := RU.T("Found: %d", 3); got != "Найдено: 3" {
		t.Errorf("RU.T = %q", got)
	}
	if got := EN.T("Found: %d", 3); got != "Found: 3" {
		t.Errorf("EN.T = %q", got)
	}
	if got := RU.T("no such key"); got != "no such key" {
		t.Errorf("fallback = %q", got)
	}
	if Parse("RU") != RU || Parse("de") != EN {
		t.Error("Parse")
	}
}
