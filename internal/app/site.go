package app

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"supportcenter/web"
)

// SiteFromEnvironment reads how the site presents itself: SITE_NAME,
// SITE_OWNER, SITE_WEBSITE, and the SITE_LOGO and SITE_ICON image files.
func SiteFromEnvironment() (web.Site, error) {
	site := web.Site{Name: strings.TrimSpace(os.Getenv("SITE_NAME")), Owner: strings.TrimSpace(os.Getenv("SITE_OWNER")), Website: os.Getenv("SITE_WEBSITE")}
	if !plainText(site.Name, 60) || !plainText(site.Owner, 100) {
		return site, errors.New("SITE_NAME and SITE_OWNER must be one line of at most 60 and 100 characters")
	}
	if site.Website != "" {
		u, err := url.Parse(site.Website)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(site.Website) > 300 {
			return site, errors.New("SITE_WEBSITE must be an http or https address")
		}
	}
	var err error
	if path := os.Getenv("SITE_LOGO"); path != "" {
		if site.Logo, err = web.LoadImage(path, "logo"); err != nil {
			return site, errors.New("SITE_LOGO " + err.Error())
		}
	}
	if path := os.Getenv("SITE_ICON"); path != "" {
		if site.Icon, err = web.LoadImage(path, "icon"); err != nil {
			return site, errors.New("SITE_ICON " + err.Error())
		}
	}
	return site, nil
}

func plainText(s string, limit int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit && strings.IndexFunc(s, unicode.IsControl) < 0
}

// BrandingHandler serves the operator's logo and icon. Pages link them with
// ?v=<content hash>, so a matching URL can be cached for a year.
func BrandingHandler(site web.Site) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveBranding(w, r, site)
	}
}

func serveBranding(w http.ResponseWriter, r *http.Request, site web.Site) {
	for _, image := range []*web.Image{site.Logo, site.Icon} {
		if image == nil || image.File != r.PathValue("file") {
			continue
		}
		w.Header().Set("Content-Type", image.Type)
		w.Header().Set("ETag", `"`+image.Version+`"`)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if r.URL.Query().Get("v") == image.Version {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(image.Data))
		return
	}
	http.NotFound(w, r)
}
