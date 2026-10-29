package app

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
)

func announcementForm(title, color string) url.Values {
	return url.Values{"csrf": {"csrf-admin"}, "title": {title}, "body": {"公告內容\n第二行"}, "color": {color}, "enabled": {"1"}}
}

func TestAnnouncementsAuthorization(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	admin, err := a.Store.User(ctx, "admin", "admin", "admin@example.test")
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.SaveAnnouncement(ctx, admin, model.Announcement{Title: "Admin notice", Body: "Content", Color: "blue", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"alice", "staff", ""} {
		for _, path := range []string{"/admin/announcements", fmt.Sprintf("/admin/announcements/%d", id)} {
			w := request(h, "GET", path, session, nil)
			want := 404
			if session == "" {
				want = 303
			}
			if w.Code != want {
				t.Fatalf("%s GET %s: %d", session, path, w.Code)
			}
		}
		for _, path := range []string{"/admin/announcements", fmt.Sprintf("/admin/announcements/%d/update", id), fmt.Sprintf("/admin/announcements/%d/disable", id), fmt.Sprintf("/admin/announcements/%d/publish", id), fmt.Sprintf("/admin/announcements/%d/delete", id)} {
			f := announcementForm("Unauthorized edit", "red")
			f.Set("version", "1")
			f.Set("csrf", "csrf-"+session)
			w := request(h, "POST", path, session, f)
			want := 404
			if session == "" {
				want = 303
			}
			if w.Code != want {
				t.Fatalf("%s POST %s: %d", session, path, w.Code)
			}
		}
	}
	for _, path := range []string{"/admin/announcements", fmt.Sprintf("/admin/announcements/%d/update", id), fmt.Sprintf("/admin/announcements/%d/disable", id), fmt.Sprintf("/admin/announcements/%d/delete", id)} {
		f := announcementForm("CSRF attempt", "red")
		f.Set("version", "1")
		f.Set("csrf", "invalid")
		if w := request(h, "POST", path, "admin", f); w.Code != 403 {
			t.Fatalf("invalid CSRF %s: %d", path, w.Code)
		}
	}
	alice.Role = "admin" // Forging the role alone cannot grant store write access.
	if _, err = a.Store.SaveAnnouncement(ctx, alice, model.Announcement{Title: "Forged", Body: "Body", Color: "blue", Enabled: true}); !errors.Is(err, database.ErrInvalid) {
		t.Fatal("forged store actor accepted", err)
	}
	if err = a.Store.ChangeAnnouncement(ctx, alice, id, 1, "delete"); err == nil {
		t.Fatal("forged actor deleted notice")
	}
	items, err := a.Store.VisibleAnnouncements(ctx)
	if err != nil || len(items) != 1 || items[0].Title != "Admin notice" {
		t.Fatal("unauthorized mutation", items, err)
	}
}

func TestAnnouncementsPublishEditAndVisibility(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	ctx := context.Background()
	for _, color := range []string{"blue", "green", "red"} {
		w := request(h, "POST", "/admin/announcements", "admin", announcementForm("Notice "+color, color))
		if w.Code != 303 || w.Header().Get("Location") != "/admin/announcements?saved=1" {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
	}
	items, err := a.Store.VisibleAnnouncements(ctx)
	if err != nil || len(items) != 3 {
		t.Fatal(items, err)
	}
	if items[0].Color != "red" || items[2].Color != "blue" {
		t.Fatal("newest notices must appear first")
	}
	draft := announcementForm("Hidden draft", "blue")
	draft.Del("enabled")
	if w := request(h, "POST", "/admin/announcements", "admin", draft); w.Code != 303 {
		t.Fatal("draft creation failed", w.Code)
	}
	if visible, e := a.Store.VisibleAnnouncements(ctx); e != nil || len(visible) != 3 {
		t.Fatal("unpublished draft visible", visible, e)
	}
	id := items[0].ID
	path := fmt.Sprintf("/admin/announcements/%d", id)
	if w := request(h, "GET", path, "admin", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `name="title" value="Notice red"`) {
		t.Fatal("edit form missing", w.Code)
	}
	f := announcementForm(`<script>alert("announcement-xss")</script>`, "green")
	f.Set("body", `<img src=x onerror=alert(1)> '); DROP TABLE users;--`)
	f.Set("version", "1")
	if w := request(h, "POST", path+"/update", "admin", f); w.Code != 303 {
		t.Fatal("edit failed", w.Code)
	}
	for _, suffix := range []string{"update", "disable", "delete"} {
		if w := request(h, "POST", path+"/"+suffix, "admin", f); w.Code != 409 || !strings.Contains(w.Body.String(), "公告已被更新") {
			t.Fatalf("stale %s: %d", suffix, w.Code)
		}
	}
	for _, tc := range []struct{ path, session string }{{"/tickets", "alice"}, {"/account", "alice"}, {"/notifications", "alice"}, {"/tickets/new", "alice"}, {"/admin/tickets", "staff"}, {"/admin/ai", "admin"}} {
		w := request(h, "GET", tc.path, tc.session, nil)
		body := w.Body.String()
		if w.Code != 200 || strings.Count(body, `class="announcement announcement-`) != 3 {
			t.Fatalf("notices missing %s: %d", tc.path, w.Code)
		}
		if strings.Contains(body, f.Get("title")) || strings.Contains(body, f.Get("body")) || !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&lt;img") {
			t.Fatal("announcement content was not escaped")
		}
		if strings.Index(body, `class="announcement-banners"`) > strings.Index(body, `class="page-heading"`) {
			t.Fatal("notices not above page content")
		}
	}
	r := httptest.NewRequest("GET", "https://support.example.test/tickets", nil)
	r.AddCookie(cookie("__Host-support-session", "alice", 86400))
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-Target", "workspace")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `class="announcement-banners"`) || strings.Contains(w.Body.String(), "<!doctype") {
		t.Fatal("HTMX workspace notices missing")
	}
	r = httptest.NewRequest("GET", "https://support.example.test/admin/announcements?saved=1", nil)
	r.AddCookie(cookie("__Host-support-session", "admin", 86400))
	r.Header.Set("HX-Request", "true")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("HX-Replace-Url") != "/admin/announcements" || !strings.Contains(w.Body.String(), "已儲存") {
		t.Fatal("HTMX save result or canonical URL missing")
	}
	if w := request(h, "GET", "/", "", nil); w.Code != 200 || strings.Contains(w.Body.String(), `class="announcement-banners"`) {
		t.Fatal("notice published to anonymous login")
	}
	if w := request(h, "POST", path+"/disable", "admin", url.Values{"csrf": {"csrf-admin"}, "version": {"2"}}); w.Code != 303 {
		t.Fatal("disable failed", w.Code)
	}
	items, err = a.Store.VisibleAnnouncements(ctx)
	if err != nil || len(items) != 2 {
		t.Fatal("disabled announcement still visible", items, err)
	}
	if w := request(h, "GET", "/admin/announcements", "admin", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "已停用") || !strings.Contains(w.Body.String(), "announcement-xss") {
		t.Fatal("disabled announcement missing from management")
	}
	if w := request(h, "POST", path+"/publish", "admin", url.Values{"csrf": {"csrf-admin"}, "version": {"3"}}); w.Code != 303 {
		t.Fatal("republish failed", w.Code)
	}
	items, err = a.Store.VisibleAnnouncements(ctx)
	if err != nil || len(items) != 3 || items[0].Version != 4 {
		t.Fatal("republish state incorrect", items, err)
	}
	if w := request(h, "POST", path+"/delete", "admin", url.Values{"csrf": {"csrf-admin"}, "version": {"4"}}); w.Code != 303 {
		t.Fatal("delete failed", w.Code)
	}
	if w := request(h, "GET", path, "admin", nil); w.Code != 404 {
		t.Fatal("deleted edit form accessible")
	}
	if w := request(h, "GET", "/admin/announcements/nope", "admin", nil); w.Code != 404 {
		t.Fatal("invalid id accepted")
	}
}

func TestAnnouncementValidationAndLimit(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	for _, tc := range []struct{ key, value string }{{"title", ""}, {"body", " "}, {"color", "orange"}, {"enabled", "true"}, {"title", strings.Repeat("字", 121)}, {"body", strings.Repeat("字", 2001)}, {"title", "bad\x00title"}, {"body", string([]byte{0xff})}} {
		f := announcementForm("Valid", "blue")
		f.Set(tc.key, tc.value)
		if w := request(h, "POST", "/admin/announcements", "admin", f); w.Code != 422 {
			t.Fatalf("invalid %s accepted: %d", tc.key, w.Code)
		}
	}
	invalidVersion := announcementForm("Invalid version", "blue")
	invalidVersion.Set("version", "")
	if w := request(h, "POST", "/admin/announcements/1/update", "admin", invalidVersion); w.Code != 422 {
		t.Fatal("missing version accepted", w.Code)
	}
	ctx := context.Background()
	admin, err := a.Store.User(ctx, "admin", "admin", "admin@example.test")
	if err != nil {
		t.Fatal(err)
	}
	v := model.Announcement{Title: strings.Repeat("字", 120), Body: strings.Repeat("字", 2000), Color: "blue", Enabled: true}
	for i := 0; i < 18; i++ {
		if _, err = a.Store.SaveAnnouncement(ctx, admin, v); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := a.Store.SaveAnnouncement(ctx, admin, v); results <- e }()
	}
	wg.Wait()
	close(results)
	ok, limit := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, database.ErrAnnouncementLimit) {
			limit++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 2 || limit != 2 {
		t.Fatalf("concurrent creation bypassed limit: %d successes %d limited", ok, limit)
	}
	items, err := a.Store.VisibleAnnouncements(ctx)
	if err != nil || len(items) != 20 {
		t.Fatal("announcement limit incorrect", len(items), err)
	}
	if w := request(h, "POST", "/admin/announcements", "admin", announcementForm("Over limit", "blue")); w.Code != 422 || !strings.Contains(w.Body.String(), "20") {
		t.Fatal("limit not shown", w.Code)
	}
}
