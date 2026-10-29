package i18n

import (
	"context"
	_ "embed"
	"net/http"
	"strconv"
	"strings"
)

type localeKey struct{}

const CookieName = "__Host-support-language"
const PreviewCookieName = "support-preview-language"

func WithLocale(ctx context.Context, locale string) context.Context {
	if locale != "en" {
		locale = "zh-Hant"
	}
	return context.WithValue(ctx, localeKey{}, locale)
}

func Lang(ctx context.Context) string {
	if value, ok := ctx.Value(localeKey{}).(string); ok && value == "en" {
		return "en"
	}
	return "zh-Hant"
}

func Detect(r *http.Request) string {
	if cookie, err := r.Cookie(CookieName); err == nil {
		if cookie.Value == "en" || cookie.Value == "zh-Hant" {
			return cookie.Value
		}
	}
	if strings.HasPrefix(r.Host, "127.0.0.1:") || strings.HasPrefix(r.Host, "localhost:") {
		if cookie, err := r.Cookie(PreviewCookieName); err == nil {
			if cookie.Value == "en" || cookie.Value == "zh-Hant" {
				return cookie.Value
			}
		}
	}
	best, score := "zh-Hant", -1.0
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		sections := strings.Split(strings.TrimSpace(part), ";")
		language := strings.ToLower(strings.TrimSpace(sections[0]))
		if language != "en" && !strings.HasPrefix(language, "en-") && language != "zh" && !strings.HasPrefix(language, "zh-") {
			continue
		}
		quality := 1.0
		for _, section := range sections[1:] {
			section = strings.TrimSpace(section)
			if strings.HasPrefix(section, "q=") {
				parsed, err := strconv.ParseFloat(strings.TrimPrefix(section, "q="), 64)
				if err != nil || parsed < 0 || parsed > 1 {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if quality > score && quality > 0 {
			score = quality
			if strings.HasPrefix(language, "en") {
				best = "en"
			} else {
				best = "zh-Hant"
			}
		}
	}
	return best
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		locale := Detect(r)
		w.Header().Set("Content-Language", locale)
		next.ServeHTTP(w, r.WithContext(WithLocale(r.Context(), locale)))
	})
}

func T(ctx context.Context, source string) string {
	if Lang(ctx) == "en" {
		if translation, ok := english[source]; ok {
			return translation
		}
	}
	return source
}

//go:embed english.tsv
var englishData string

var english = func() map[string]string {
	translations := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(englishData), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			panic("invalid English translation line")
		}
		if _, exists := translations[parts[0]]; exists {
			panic("duplicate English translation")
		}
		translations[parts[0]] = parts[1]
	}
	return translations
}()
