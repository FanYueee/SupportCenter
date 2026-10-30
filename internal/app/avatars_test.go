package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

type avatarTransport func(*http.Request) (*http.Response, error)

func (f avatarTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGravatarHashAndCache(t *testing.T) {
	want := "84059b07d4be67b806386c0aad8070a23f18836bbaae342275dc0a83414c32ee"
	if got := gravatarHash("  MyEmailAddress@example.com  "); got != want {
		t.Fatalf("hash = %s", got)
	}
	png := []byte("\x89PNG\r\n\x1a\nexample")
	calls := 0
	a := &App{AvatarHTTP: &http.Client{Transport: avatarTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://gravatar.com/avatar/"+want+"?s=64&r=g&d=404" {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(png))}, nil
	})}}
	for range 2 {
		image := a.gravatar(context.Background(), want)
		if image.mime != "image/png" || !bytes.Equal(image.data, png) {
			t.Fatalf("unexpected image: %+v", image)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one upstream request, got %d", calls)
	}
}

func TestGravatarFallback(t *testing.T) {
	a := &App{AvatarHTTP: &http.Client{Transport: avatarTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	image := a.gravatar(context.Background(), gravatarHash("missing@example.test"))
	if len(image.data) != 0 {
		t.Fatal("missing avatar should use local fallback")
	}
}

func TestGravatarIsAlwaysUsed(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	calls := 0
	a.AvatarHTTP = &http.Client{Transport: avatarTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("\x89PNG\r\n\x1a\nphoto")))}, nil
	})}
	if got := request(h, "GET", "/avatars/me", "alice", nil); got.Code != 200 || got.Header().Get("Content-Type") != "image/png" || calls != 1 {
		t.Fatalf("gravatar avatar: %d, %s, fetches %d", got.Code, got.Header().Get("Content-Type"), calls)
	}
	if got := request(h, "GET", "/avatars/gravatar-preview", "alice", nil); got.Code != 404 {
		t.Fatalf("removed avatar preview endpoint: %d", got.Code)
	}
}

func TestAvatarAccessFollowsVisibleMessages(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	a.AvatarHTTP = &http.Client{Transport: avatarTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "Private", Category: "technical", Priority: "low"}, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerAvatar := fmt.Sprintf("/tickets/%d/avatars/%d", id, alice.ID)
	for _, tc := range []struct {
		path, session string
		status        int
	}{
		{"/avatars/me", "", 303}, {"/avatars/me", "alice", 200},
		{ownerAvatar, "alice", 200}, {ownerAvatar, "bob", 404}, {ownerAvatar, "staff", 200},
		{fmt.Sprintf("/tickets/%d/avatars/%d", id, bob.ID), "alice", 404},
	} {
		res := request(h, "GET", tc.path, tc.session, nil)
		if res.Code != tc.status {
			t.Fatalf("GET %s as %s = %d, want %d", tc.path, tc.session, res.Code, tc.status)
		}
		if res.Code == 200 && (!strings.HasPrefix(res.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(res.Body.String(), "<svg")) {
			t.Fatalf("GET %s did not return a fallback image", tc.path)
		}
	}
	if err := a.Store.Reply(context.Background(), staff, id, true, "private note", true, nil); err != nil {
		t.Fatal(err)
	}
	staffAvatar := "/tickets/" + strconv.FormatInt(id, 10) + "/avatars/" + strconv.FormatInt(staff.ID, 10)
	if res := request(h, "GET", staffAvatar, "alice", nil); res.Code != 404 {
		t.Fatalf("private-note author visible to customer: %d", res.Code)
	}
	if res := request(h, "GET", staffAvatar, "staff", nil); res.Code != 200 {
		t.Fatalf("private-note author hidden from staff: %d", res.Code)
	}
	if err := a.Store.Reply(context.Background(), staff, id, true, "public reply", false, nil); err != nil {
		t.Fatal(err)
	}
	if res := request(h, "GET", staffAvatar, "alice", nil); res.Code != 200 {
		t.Fatalf("public author hidden from customer: %d", res.Code)
	}
}
