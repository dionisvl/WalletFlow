// Package i18n translates UI text. Keys are the English text itself,
// so English needs no dictionary and a missing translation falls back to it.
package i18n

import (
	"fmt"
	"strings"
)

// Lang is a UI language code.
type Lang string

const (
	EN Lang = "en"
	RU Lang = "ru"
)

// Langs lists the supported languages, default first.
var Langs = []Lang{EN, RU}

var dicts = map[Lang]map[string]string{RU: ru}

// Parse returns the language for a code, or EN.
func Parse(s string) Lang {
	l := Lang(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := dicts[l]; ok {
		return l
	}
	return EN
}

// Name is the language's own name, for the switcher.
func (l Lang) Name() string {
	if l == RU {
		return "Русский"
	}
	return "English"
}

// T translates s and, with args, formats it like fmt.Sprintf.
func (l Lang) T(s string, args ...any) string {
	if tr, ok := dicts[l][s]; ok {
		s = tr
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Dict returns the raw dictionary of l (nil for English), for the browser side.
func (l Lang) Dict() map[string]string { return dicts[l] }
