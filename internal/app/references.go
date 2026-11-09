package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/web"
)

// References asks the Portal for the items, such as invoices and services,
// that tickets may tag. Requests are signed like the Portal's own calls to
// this site, under the issuer referenceIssuer, which this site never accepts,
// so neither side can replay the other's requests.
type References struct {
	URL    string // the endpoints' base, e.g. https://portal.example.com/api/support/v1/references
	Key    []byte
	Host   string // this site's hostname, which the Portal checks
	Client *http.Client

	mu      sync.Mutex
	kinds   []model.ReferenceKind
	fetched time.Time // when kinds were last asked for, successfully or not
}

const referenceIssuer = "support-center"

// Kinds are cached for ten minutes. When the Portal cannot answer, the last
// good list stays; without one, it is asked again a minute later.
const (
	referenceKindsFresh = 10 * time.Minute
	referenceKindsRetry = time.Minute
	referenceSearchMax  = 8
	referenceItemsMax   = 10
)

// ReferencesFromEnvironment turns on tagging when PORTAL_REFERENCES_URL is set.
// It signs with the Portal API's key, so that must be set too.
func ReferencesFromEnvironment(origin string) (*References, error) {
	base := strings.TrimRight(os.Getenv("PORTAL_REFERENCES_URL"), "/")
	if base == "" {
		return nil, nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PORTAL_REFERENCES_URL must be an https address")
	}
	secret := os.Getenv("PORTAL_API_SIGNING_KEY")
	if !portalHex64.MatchString(secret) {
		return nil, errors.New("PORTAL_REFERENCES_URL needs PORTAL_API_SIGNING_KEY")
	}
	key, _ := hex.DecodeString(secret)
	return &References{URL: base, Key: key, Host: originHost(origin), Client: &http.Client{Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// referenceActor names whose items to look up (customer) and who is asking.
type referenceActor struct {
	Hostname   string `json:"hostname"`
	Customer   string `json:"customer,omitempty"`
	Viewer     string `json:"viewer,omitempty"`
	ViewerRole string `json:"viewerRole,omitempty"`
}

// get calls one endpoint and decodes its JSON answer into out.
func (s *References) get(ctx context.Context, path string, query url.Values, actor referenceActor, out any) error {
	target := s.URL + path
	if len(query) > 0 {
		// The Portal drops a lone "?", which would break the signature.
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(actor)
	random := make([]byte, 16)
	rand.Read(random)
	stamp, nonce, encoded := fmt.Sprint(time.Now().Unix()), hex.EncodeToString(random), base64.RawURLEncoding.EncodeToString(raw)
	for name, value := range map[string]string{"X-Portal-Issuer": referenceIssuer, "X-Portal-Timestamp": stamp, "X-Portal-Nonce": nonce, "X-Portal-Actor": encoded,
		"X-Portal-Body-SHA256": emptyBodySHA256, "X-Portal-Signature": referenceSignature(s.Key, req.URL.RequestURI(), stamp, nonce, encoded)} {
		req.Header.Set(name, value)
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("portal references: status %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(out)
}

// emptyBodySHA256 is the digest of the empty body every GET has.
const emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// referenceSignature signs a GET the way the Portal signs its calls here.
func referenceSignature(key []byte, uri, stamp, nonce, actor string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(strings.Join([]string{"vproxy-support-v1", referenceIssuer, http.MethodGet, uri, stamp, nonce, actor, "", emptyBodySHA256}, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

// Kinds are what messages may tag; none when the Portal cannot say.
func (s *References) Kinds(ctx context.Context) []model.ReferenceKind {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	age := time.Since(s.fetched)
	if age < referenceKindsFresh && (s.kinds != nil || age < referenceKindsRetry) {
		return s.kinds
	}
	s.fetched = time.Now()
	var answer struct {
		Kinds []model.ReferenceKind `json:"kinds"`
	}
	if err := s.get(ctx, "/kinds", nil, referenceActor{Hostname: s.Host}, &answer); err != nil {
		return s.kinds
	}
	kinds := []model.ReferenceKind{}
	for _, kind := range answer.Kinds {
		if model.ReferenceKey.MatchString(kind.Key) && len(kind.Trigger) == 1 && strings.Contains(model.ReferenceTriggers, kind.Trigger) &&
			shownText(kind.Label, true, 20) && shownText(kind.LabelEn, false, 20) && !slices.ContainsFunc(kinds, func(k model.ReferenceKind) bool { return k.Key == kind.Key || k.Trigger == kind.Trigger }) {
			kinds = append(kinds, kind)
		}
	}
	s.kinds = kinds
	return kinds
}

// Kind finds the kind a trigger starts, if any.
func (s *References) Kind(ctx context.Context, trigger string) (model.ReferenceKind, bool) {
	for _, kind := range s.Kinds(ctx) {
		if kind.Trigger == trigger {
			return kind, true
		}
	}
	return model.ReferenceKind{}, false
}

// referenceItem is one item as the Portal sends it.
type referenceItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	CustomerURL string `json:"customerUrl"`
	StaffURL    string `json:"staffUrl"`
}

// Search finds the customer's items of a kind matching q, best first.
func (s *References) Search(ctx context.Context, kind model.ReferenceKind, customer, viewer, role, q string) ([]model.Reference, error) {
	query := url.Values{"kind": {kind.Key}, "q": {q}, "limit": {fmt.Sprint(referenceSearchMax)}}
	return s.items(ctx, "/search", query, kind, referenceActor{Hostname: s.Host, Customer: customer, Viewer: viewer, ViewerRole: role}, referenceSearchMax)
}

// Items keeps the ids that name the customer's items of a kind, as they are now.
func (s *References) Items(ctx context.Context, kind model.ReferenceKind, customer, viewer, role string, ids []string) ([]model.Reference, error) {
	query := url.Values{"kind": {kind.Key}, "ids": {strings.Join(ids, ",")}}
	refs, err := s.items(ctx, "/items", query, kind, referenceActor{Hostname: s.Host, Customer: customer, Viewer: viewer, ViewerRole: role}, referenceItemsMax)
	// Only what was asked for, whatever the answer holds.
	return slices.DeleteFunc(refs, func(ref model.Reference) bool { return !slices.Contains(ids, ref.ID) }), err
}

func (s *References) items(ctx context.Context, path string, query url.Values, kind model.ReferenceKind, actor referenceActor, max int) ([]model.Reference, error) {
	var answer struct {
		Items []referenceItem `json:"items"`
	}
	if err := s.get(ctx, path, query, actor, &answer); err != nil {
		return nil, err
	}
	refs := []model.Reference{}
	for _, item := range answer.Items {
		if len(refs) == max {
			break
		}
		if !model.ReferenceID.MatchString(item.ID) || !shownText(item.Title, true, 80) || !shownText(item.Detail, false, 120) || !s.sameSite(item.CustomerURL) || !s.sameSite(item.StaffURL) ||
			slices.ContainsFunc(refs, func(ref model.Reference) bool { return ref.ID == item.ID }) {
			continue
		}
		refs = append(refs, model.Reference{ReferenceKind: kind, ID: item.ID, Title: item.Title, Detail: item.Detail, CustomerURL: item.CustomerURL, StaffURL: item.StaffURL})
	}
	return refs, nil
}

// sameSite accepts only https links on the Portal's own host.
func (s *References) sameSite(link string) bool {
	u, err := url.Parse(link)
	base, _ := url.Parse(s.URL)
	return err == nil && len(link) <= 2048 && u.Scheme == "https" && u.User == nil && strings.EqualFold(u.Host, base.Host) && !strings.ContainsAny(link, " \t\r\n\"'<>\\")
}

// shownText is a single line of up to max characters without control
// characters, and not blank when required.
func shownText(s string, required bool, max int) bool {
	return plainText(s, max) && (!required || strings.TrimSpace(s) != "")
}

// referenceSearch suggests items to tag while a message is typed: the
// customer's own, or, on a ticket staff are answering, its owner's.
func (a *App) referenceSearch(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	kind, ok := a.References.Kind(r.Context(), r.URL.Query().Get("trigger"))
	if !ok {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	owner, role := u.ID, "customer"
	if r.PathValue("id") != "" {
		id, ok := idFrom(r)
		if !ok {
			a.problem(w, r, 404, "找不到客服單。")
			return
		}
		ticket, err := a.Store.Ticket(r.Context(), u, id, true)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		owner, role = ticket.UserID, "staff"
	}
	customer, err := a.Store.VerifiedEmail(r.Context(), owner)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var refs []model.Reference
	failed := false
	if customer != "" && utf8.RuneCountInString(q) <= 40 {
		if refs, err = a.References.Search(r.Context(), kind, customer, strings.ToLower(u.Email), role, q); err != nil {
			a.Log.Warn("portal reference search failed", "error", err)
			failed = true
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.ReferenceOptions(kind, refs, q, failed).Render(r.Context(), w)
}

// tagged checks with the Portal the items a message tags, and any named
// outright, by kind key. Only the ticket owner's count, as they are now. The
// message never waits long on the Portal; a tag it cannot confirm stays text.
func (a *App) tagged(ctx context.Context, owner int64, viewer model.User, body string, named map[string][]string) []model.Reference {
	kinds := a.References.Kinds(ctx)
	wanted := map[string][]string{}
	add := func(kind, id string) {
		if !slices.Contains(wanted[kind], id) && len(wanted[kind]) < referenceItemsMax {
			wanted[kind] = append(wanted[kind], id)
		}
	}
	for kind, ids := range named {
		for _, id := range ids {
			add(kind, id)
		}
	}
	for _, span := range model.ReferenceSpans(body) {
		for _, kind := range kinds {
			if kind.Trigger == span.Trigger {
				add(kind.Key, span.ID)
			}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	customer, err := a.Store.VerifiedEmail(ctx, owner)
	if err != nil || customer == "" {
		return nil
	}
	role := "customer"
	if viewer.ID != owner && viewer.Staff() {
		role = "staff"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var refs []model.Reference
	for _, kind := range kinds {
		if len(wanted[kind.Key]) == 0 {
			continue
		}
		found, err := a.References.Items(ctx, kind, customer, strings.ToLower(viewer.Email), role, wanted[kind.Key])
		if err != nil {
			a.Log.Warn("portal reference check failed", "error", err)
		}
		refs = append(refs, found...)
	}
	return refs
}

// withTags readies a message's context: the tagged items it carries into the
// transaction that stores the message.
func (a *App) withTags(ctx context.Context, owner int64, viewer model.User, body string, named map[string][]string) context.Context {
	if a.References == nil {
		return ctx
	}
	return database.WithReferences(ctx, a.tagged(ctx, owner, viewer, body, named))
}
