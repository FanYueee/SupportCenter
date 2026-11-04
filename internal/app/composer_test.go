package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func TestCannedRepliesAreKeptByAdminsAndOfferedToStaff(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	form := url.Values{"csrf": {"csrf-admin"}, "title": {"請提供截圖"}, "body": {"您好，請提供 <b>錯誤畫面</b>。"}}
	if got := request(h, "GET", "/admin/canned", "staff", nil); got.Code != http.StatusNotFound {
		t.Fatalf("support opened canned settings: %d", got.Code)
	}
	if got := request(h, "POST", "/admin/canned", "staff", url.Values{"csrf": {"csrf-staff"}, "title": {"x"}, "body": {"y"}}); got.Code != http.StatusNotFound {
		t.Fatalf("support saved a canned reply: %d", got.Code)
	}
	if got := request(h, "POST", "/admin/canned", "admin", url.Values{"csrf": {"csrf-admin"}, "title": {" "}, "body": {"y"}}); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty title accepted: %d", got.Code)
	}
	if got := request(h, "POST", "/admin/canned", "admin", form); got.Code != http.StatusSeeOther {
		t.Fatalf("save canned reply: %d", got.Code)
	}
	replies, err := a.Store.CannedReplies(ctx)
	if err != nil || len(replies) != 1 {
		t.Fatalf("canned replies: %+v %v", replies, err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "需要協助", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	staffPage := request(h, "GET", fmt.Sprintf("/admin/tickets/%d", id), "staff", nil).Body.String()
	if !strings.Contains(staffPage, `data-canned="您好，請提供 &lt;b&gt;錯誤畫面&lt;/b&gt;。"`) || strings.Contains(staffPage, `href="/admin/canned"`) {
		t.Fatal("support should get the canned replies, escaped, without the settings link")
	}
	if page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String(); strings.Contains(page, "data-canned") || strings.Contains(page, "請提供截圖") {
		t.Fatal("customers must not see canned replies")
	}
	path := fmt.Sprintf("/admin/canned/%d", replies[0].ID)
	if got := request(h, "GET", path, "admin", nil); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `value="請提供截圖"`) {
		t.Fatalf("edit page: %d", got.Code)
	}
	form.Set("title", "請提供錯誤截圖")
	form.Set("version", "1")
	if got := request(h, "POST", path+"/update", "admin", form); got.Code != http.StatusSeeOther {
		t.Fatalf("update: %d", got.Code)
	}
	if got := request(h, "POST", path+"/update", "admin", form); got.Code != http.StatusConflict {
		t.Fatalf("stale update accepted: %d", got.Code)
	}
	if got := request(h, "POST", path+"/delete", "admin", url.Values{"csrf": {"csrf-admin"}, "version": {"2"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", got.Code)
	}
	if replies, _ = a.Store.CannedReplies(ctx); len(replies) != 0 {
		t.Fatalf("not deleted: %+v", replies)
	}
}

var activityValue = regexp.MustCompile(`id="activity-signal"[^>]*data-activity="([0-9]+)"`)

func TestPagesPollForRepliesWorthAChime(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	latest := func(user string) string {
		t.Helper()
		m := activityValue.FindStringSubmatch(request(h, "GET", "/tickets", user, nil).Body.String())
		if m == nil {
			t.Fatal("page has no activity poll")
		}
		return m[1]
	}
	changed := func(user, since string) bool {
		t.Helper()
		got := request(h, "GET", "/activity?since="+since, user, nil)
		if got.Code == http.StatusNoContent {
			return false
		}
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `hx-trigger="every 20s"`) {
			t.Fatalf("activity: %d %s", got.Code, got.Body.String())
		}
		return true
	}
	aliceSeen, staffSeen := latest("alice"), latest("staff")
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "帳務問題", Category: "billing", Priority: "low"}, "請協助", nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed("alice", aliceSeen) || !changed("staff", staffSeen) {
		t.Fatal("a new ticket should chime for staff only")
	}
	staffSeen = latest("staff")
	if err = a.Store.Reply(ctx, staff, id, true, "內部紀錄", true, nil); err != nil {
		t.Fatal(err)
	}
	if changed("alice", aliceSeen) {
		t.Fatal("internal notes must not reach the customer")
	}
	if err = a.Store.Reply(ctx, staff, id, true, "請提供帳號", false, nil); err != nil {
		t.Fatal(err)
	}
	if !changed("alice", aliceSeen) || changed("bob", latest("bob")) || changed("staff", staffSeen) {
		t.Fatal("a staff reply should chime for its customer only")
	}
	// Staff in a department hear only its tickets and those assigned to them.
	if got := request(h, "POST", fmt.Sprintf("/admin/departments/%d", staff.ID), "admin", url.Values{"csrf": {"csrf-admin"}, "departments": {"technical"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("departments: %d", got.Code)
	}
	staffSeen = latest("staff")
	if err = a.Store.Reply(ctx, alice, id, false, "帳號是 alice", false, nil); err != nil {
		t.Fatal(err)
	}
	if changed("staff", staffSeen) || !changed("admin", "0") {
		t.Fatal("a billing reply should not chime for technical staff")
	}
	if _, err = a.Store.Pool.Exec(ctx, `INSERT INTO ticket_assignees(ticket_id,user_id) VALUES($1,$2)`, id, staff.ID); err != nil {
		t.Fatal(err)
	}
	if !changed("staff", staffSeen) {
		t.Fatal("an assigned ticket should chime")
	}
	// Without a session the poll stops instead of sending the page away.
	if got := request(h, "GET", "/activity?since=1", "", nil); got.Code != 286 || got.Header().Get("HX-Redirect") != "" {
		t.Fatalf("signed out poll: %d", got.Code)
	}
}

func TestMessagesRenderMarkdownSafely(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "格式", Category: "other", Priority: "low"}, "**重要** <script>alert(1)</script>\n- 第一步\n- `rm -rf <dir>`", nil)
	if err != nil {
		t.Fatal(err)
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String()
	if !strings.Contains(page, "<strong>重要</strong> &lt;script&gt;alert(1)&lt;/script&gt;</p><ul><li>第一步</li><li><code>rm -rf &lt;dir&gt;</code></li></ul>") || strings.Contains(page, "<script>alert(1)") {
		t.Fatal("message Markdown not rendered safely")
	}
	if !strings.Contains(page, `data-format="bold"`) || !strings.Contains(page, `data-mention-search="/tickets/mentions?except=`) {
		t.Fatal("reply box has no formatting bar")
	}
}
