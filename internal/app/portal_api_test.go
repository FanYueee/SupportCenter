package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"supportcenter/internal/model"
)

const portalTestHost = "portal.example.test"

// portalRequest signs a request the way the Portal does.
func portalRequest(t *testing.T, h http.Handler, key []byte, method, path string, actor portalActor) *httptest.ResponseRecorder {
	return portalJSONRequest(t, h, key, method, path, actor, nil)
}

// portalJSONRequest signs a request with a JSON body; a nil body sends none.
func portalJSONRequest(t *testing.T, h http.Handler, key []byte, method, path string, actor portalActor, input any) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	contentType := ""
	if input != nil {
		var err error
		if body, err = json.Marshal(input); err != nil {
			t.Fatal(err)
		}
		contentType = "application/json"
	}
	return portalSignedRequest(t, h, key, "vproxy-portal", method, path, actor, contentType, body)
}

// portalSignedRequest signs any body under issuer.
func portalSignedRequest(t *testing.T, h http.Handler, key []byte, issuer, method, path string, actor portalActor, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(actor)
	if err != nil {
		t.Fatal(err)
	}
	actorRaw := base64.RawURLEncoding.EncodeToString(raw)
	nonce := make([]byte, 16)
	rand.Read(nonce)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	digest := sha256.Sum256(body)
	r := httptest.NewRequest(method, "https://support.example.test"+path, bytes.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	message := strings.Join([]string{"vproxy-support-v1", issuer, method, r.URL.RequestURI(), stamp, hex.EncodeToString(nonce), actorRaw, contentType, hex.EncodeToString(digest[:])}, "\n")
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	r.Header.Set("X-Portal-Issuer", issuer)
	r.Header.Set("X-Portal-Timestamp", stamp)
	r.Header.Set("X-Portal-Nonce", hex.EncodeToString(nonce))
	r.Header.Set("X-Portal-Actor", actorRaw)
	r.Header.Set("X-Portal-Body-SHA256", hex.EncodeToString(digest[:]))
	r.Header.Set("X-Portal-Signature", hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func portalTestApp(t *testing.T) (*App, http.Handler, []byte, model.User, model.User) {
	a, _, alice, _, staff := testApp(t)
	key := make([]byte, 32)
	rand.Read(key)
	a.PortalAPI = &PortalAPIConfig{Key: key, Issuer: "vproxy-portal", Hosts: map[string]bool{portalTestHost: true}}
	return a, a.Handler(), key, alice, staff
}

func usersWithEmail(t *testing.T, a *App, email string) []string {
	t.Helper()
	rows, err := a.Store.Pool.Query(context.Background(), `SELECT auth0_id FROM users WHERE lower(trim(email))=lower($1) ORDER BY id`, email)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var subjects []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		subjects = append(subjects, s)
	}
	return subjects
}

func TestPortalMatchesUsersByEmail(t *testing.T) {
	a, h, key, alice, _ := portalTestApp(t)
	ctx := context.Background()
	member := func(subject, email, name string) int {
		return portalRequest(t, h, key, "GET", "/api/portal/v1/tickets", portalActor{Subject: subject, Email: email, Name: name, Hostname: portalTestHost}).Code
	}
	// An existing account is reused whatever subject the Portal sends, and keeps its own data.
	for _, subject := range []string{"auth0|portal-a", "google-oauth2|portal-b", ""} {
		if code := member(subject, alice.Email, "Portal Name"); code != 200 {
			t.Fatalf("subject %q: %d", subject, code)
		}
	}
	// Email case and surrounding spaces do not create another person.
	if code := member("x|upper", strings.ToUpper(alice.Email), "Portal Name"); code != 200 {
		t.Fatal("mixed-case email rejected", code)
	}
	if subjects := usersWithEmail(t, a, alice.Email); len(subjects) != 1 || subjects[0] != "alice" {
		t.Fatalf("existing account duplicated or rewritten: %v", subjects)
	}
	var name, role string
	a.Store.Pool.QueryRow(ctx, `SELECT name,role FROM users WHERE id=$1`, alice.ID).Scan(&name, &role)
	if name != alice.Name || role != "user" {
		t.Fatalf("existing account overwritten: %s %s", name, role)
	}

	// A new address creates one verified Portal account, also across subjects and case.
	if code := member("auth0|new-1", "new.person@example.test", "New Person"); code != 200 {
		t.Fatal("new email rejected", code)
	}
	if code := member("other|new-2", "New.Person@Example.Test", "New Person"); code != 200 {
		t.Fatal("new email, other subject rejected", code)
	}
	var verified bool
	a.Store.Pool.QueryRow(ctx, `SELECT role,email_verified FROM users WHERE auth0_id='portal:new.person@example.test'`).Scan(&role, &verified)
	if subjects := usersWithEmail(t, a, "new.person@example.test"); len(subjects) != 1 || subjects[0] != "portal:new.person@example.test" || role != "user" || !verified {
		t.Fatalf("new Portal account: %v %s %v", subjects, role, verified)
	}
}

func TestPortalUserIsCreatedOnceUnderConcurrency(t *testing.T) {
	a, _, _, _, _ := portalTestApp(t)
	var wg sync.WaitGroup
	ids := make([]int64, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user, err := a.Store.PortalUser(context.Background(), "Racer", "racer@example.test")
			if err != nil {
				t.Error(err)
			}
			ids[i] = user.ID
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] || id == 0 {
			t.Fatalf("concurrent requests resolved different accounts: %v", ids)
		}
	}
	if subjects := usersWithEmail(t, a, "racer@example.test"); len(subjects) != 1 {
		t.Fatalf("duplicate accounts: %v", subjects)
	}
}

func TestPortalBindsPendingInvitesByEmail(t *testing.T) {
	a, h, key, alice, _ := portalTestApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "shared", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Invite(ctx, alice, id, "carol@example.test"); err != nil {
		t.Fatal(err)
	}
	res := portalRequest(t, h, key, "GET", "/api/portal/v1/tickets/"+strconv.FormatInt(id, 10), portalActor{Email: "Carol@Example.Test", Name: "Carol", Hostname: portalTestHost})
	if res.Code != 200 {
		t.Fatal("invited Portal user cannot open the ticket", res.Code)
	}
	var bound int64
	a.Store.Pool.QueryRow(ctx, `SELECT COALESCE(user_id,0) FROM ticket_invites WHERE ticket_id=$1`, id).Scan(&bound)
	if bound == 0 {
		t.Fatal("pending invite was not bound to the Portal account")
	}
}

func TestPortalStaffAccessNeedsPortalFlagAndStaffRole(t *testing.T) {
	_, h, key, alice, staff := portalTestApp(t)
	staffList := func(email string, canStaff bool) int {
		return portalRequest(t, h, key, "GET", "/api/portal/v1/staff/tickets", portalActor{Email: email, Name: "Someone", Hostname: portalTestHost, CanStaff: canStaff}).Code
	}
	if code := staffList(alice.Email, true); code != http.StatusForbidden {
		t.Fatal("ordinary user reached the staff API", code)
	}
	if code := staffList("stranger@example.test", true); code != http.StatusForbidden {
		t.Fatal("new Portal account reached the staff API", code)
	}
	if code := staffList(staff.Email, false); code != http.StatusForbidden {
		t.Fatal("staff role without the Portal staff flag reached the staff API", code)
	}
	if code := staffList(staff.Email, true); code != 200 {
		t.Fatal("support staff blocked from the staff API", code)
	}
}

func TestPortalActorValidation(t *testing.T) {
	_, h, key, alice, _ := portalTestApp(t)
	for name, actor := range map[string]portalActor{
		"system subject":   {Subject: "system:ai", Email: alice.Email, Name: "x", Hostname: portalTestHost},
		"control subject":  {Subject: "abc\u0007def", Email: alice.Email, Name: "x", Hostname: portalTestHost},
		"short subject":    {Subject: "ab", Email: alice.Email, Name: "x", Hostname: portalTestHost},
		"dev subject":      {Subject: "portal-dev|1", Email: alice.Email, Name: "x", Hostname: portalTestHost},
		"invalid email":    {Email: "not-an-email", Name: "x", Hostname: portalTestHost},
		"missing email":    {Name: "x", Hostname: portalTestHost},
		"unknown hostname": {Email: alice.Email, Name: "x", Hostname: "evil.example.test"},
	} {
		if code := portalRequest(t, h, key, "GET", "/api/portal/v1/tickets", actor).Code; code != http.StatusUnauthorized {
			t.Errorf("%s accepted: %d", name, code)
		}
	}
	// A bad signature never reaches account lookup.
	wrong := make([]byte, 32)
	if code := portalRequest(t, h, wrong, "GET", "/api/portal/v1/tickets", portalActor{Email: "forged@example.test", Name: "x", Hostname: portalTestHost}).Code; code != http.StatusUnauthorized {
		t.Fatal("forged signature accepted", code)
	}
}

func TestPortalParticipantsShowOnlyEmail(t *testing.T) {
	a, h, key, alice, _ := portalTestApp(t)
	ctx := context.Background()
	if _, err := a.Store.User(ctx, "dave", "Dave Registered", "dave@example.test", true); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "shared", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/portal/v1/tickets/" + strconv.FormatInt(id, 10)
	owner := portalActor{Email: alice.Email, Name: "Alice", Hostname: portalTestHost}
	// Registered and unregistered addresses get the same status and body.
	registered := portalJSONRequest(t, h, key, "POST", path+"/invite", owner, map[string]any{"email": "dave@example.test"})
	unregistered := portalJSONRequest(t, h, key, "POST", path+"/invite", owner, map[string]any{"email": "erin@example.test"})
	if registered.Code != 200 || registered.Code != unregistered.Code || registered.Body.String() != unregistered.Body.String() {
		t.Fatalf("invite responses differ: %d %s / %d %s", registered.Code, registered.Body, unregistered.Code, unregistered.Body)
	}
	res := portalRequest(t, h, key, "GET", path, owner)
	var detail struct {
		Participants []map[string]any `json:"participants"`
	}
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &detail) != nil {
		t.Fatal("ticket detail", res.Code, res.Body)
	}
	if len(detail.Participants) != 2 || len(detail.Participants[0]) != 1 || len(detail.Participants[1]) != 1 ||
		detail.Participants[0]["email"] != "dave@example.test" || detail.Participants[1]["email"] != "erin@example.test" {
		t.Fatalf("participants must carry only an email: %v", detail.Participants)
	}
	if strings.Contains(res.Body.String(), "Dave Registered") {
		t.Fatal("participant account name leaked")
	}
}

func TestUserListShowsRegistrationAndPortalUse(t *testing.T) {
	a, h, key, _, _ := portalTestApp(t)
	ctx := context.Background()
	actor := portalActor{Email: "portal.person@example.test", Name: "Portal Person", Hostname: portalTestHost}
	if code := portalRequest(t, h, key, "GET", "/api/portal/v1/tickets", actor).Code; code != 200 {
		t.Fatal("portal request", code)
	}
	seen := func() (at time.Time) {
		a.Store.Pool.QueryRow(ctx, `SELECT portal_seen_at FROM users WHERE auth0_id='portal:portal.person@example.test'`).Scan(&at)
		return
	}
	first := seen()
	if first.IsZero() {
		t.Fatal("Portal use was not recorded")
	}
	// Busy Portal sessions do not write on every call.
	portalRequest(t, h, key, "GET", "/api/portal/v1/tickets", actor)
	if !seen().Equal(first) {
		t.Fatal("Portal use rewritten within five minutes")
	}
	a.Store.Pool.Exec(ctx, `UPDATE users SET portal_seen_at=now()-interval '10 minutes' WHERE auth0_id='portal:portal.person@example.test'`)
	portalRequest(t, h, key, "GET", "/api/portal/v1/tickets", actor)
	if !seen().After(first.Add(-time.Minute)) {
		t.Fatal("later Portal use not recorded")
	}
	users, _, err := a.Store.Users(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.CreatedAt.IsZero() {
			t.Fatalf("%s has no registration date", u.User.Email)
		}
		if last, portal := u.LastSeen(); u.User.Email == actor.Email && (last == nil || !portal) {
			t.Fatal("Portal user shows no recent use")
		}
	}
	page := request(h, "GET", "/admin/users", "admin", nil).Body.String()
	if !strings.Contains(page, `data-label="註冊日期"`) || !strings.Contains(page, ">Portal</span>") || !strings.Contains(page, `data-local-time="date"`) {
		t.Fatal("user list misses the registration date or Portal use")
	}
}
