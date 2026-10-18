package web

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

func templateFuncs(asset func(string) string) template.FuncMap {
	return template.FuncMap{
		"asset":    asset,
		"langName": languageName,
		"icon": func(name string) template.HTML {
			return template.HTML(`<svg class="icon" aria-hidden="true" focusable="false"><use href="#i-` +
				template.HTMLEscapeString(name) + `"></use></svg>`)
		},
		"plural": func(n int, singular, plural string) string {
			if n == 1 {
				return fmt.Sprintf("%d %s", n, singular)
			}
			return fmt.Sprintf("%d %s", n, plural)
		},
	}
}

// languageNames are the endonyms shown in language menus.
var languageNames = map[string]string{
	"ar": "العربية", "de": "Deutsch", "en": "English", "es": "Español", "fa": "فارسی",
	"fr": "Français", "it": "Italiano", "nl": "Nederlands", "pt": "Português", "ru": "Русский",
}

func languageName(code string) string {
	if n, ok := languageNames[code]; ok {
		return n
	}
	return strings.ToUpper(code)
}

var germanMonths = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli",
	"August", "September", "Oktober", "November", "Dezember"}

// formatDate renders a publication date in the conventions of the language.
func formatDate(t time.Time, lang string) string {
	if t.IsZero() {
		return ""
	}
	if lang == "de" {
		return fmt.Sprintf("%d. %s %d", t.Day(), germanMonths[t.Month()-1], t.Year())
	}
	return t.Format("2 January 2006")
}

// fold normalizes text for forgiving matching: case-insensitive, without
// diacritics, and with typographic apostrophes treated as plain ones, so
// "bahaullah" finds "Bahá’u’lláh".
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		out = s
	}
	out = strings.NewReplacer("’", "", "‘", "", "'", "", "‑", "-").Replace(out)
	return strings.ToLower(out)
}

// withQuery returns u with the given query parameters set (empty values are
// removed).
func withQuery(u *url.URL, params ...string) string {
	q := u.Query()
	for i := 0; i+1 < len(params); i += 2 {
		if params[i+1] == "" {
			q.Del(params[i])
		} else {
			q.Set(params[i], params[i+1])
		}
	}
	out := url.URL{Path: u.Path, RawQuery: q.Encode()}
	return out.String()
}
