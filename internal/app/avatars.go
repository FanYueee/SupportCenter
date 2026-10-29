package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"supportcenter/internal/model"
)

type avatarImage struct {
	data    []byte
	mime    string
	expires time.Time
}

type avatarCache struct {
	mu     sync.Mutex
	images map[string]avatarImage
	group  singleflight.Group
}

func gravatarHash(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

var defaultAvatarHTTP = &http.Client{
	Timeout: 3 * time.Second,
	CheckRedirect: func(r *http.Request, via []*http.Request) error {
		host := strings.ToLower(r.URL.Hostname())
		if len(via) > 3 || r.URL.Scheme != "https" || (host != "gravatar.com" && !strings.HasSuffix(host, ".gravatar.com")) {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

func (a *App) myAvatar(w http.ResponseWriter, r *http.Request) {
	a.writeAvatar(w, r, current(r).User)
}

func (a *App) ticketAvatar(w http.ResponseWriter, r *http.Request) {
	ticketID, ok := idFrom(r)
	authorID, err := strconv.ParseInt(r.PathValue("author"), 10, 64)
	if !ok || err != nil || authorID < 1 {
		http.NotFound(w, r)
		return
	}
	author, err := a.Store.AvatarAuthor(r.Context(), current(r).User, ticketID, authorID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.writeAvatar(w, r, author)
}

func (a *App) writeAvatar(w http.ResponseWriter, r *http.Request, user model.User) {
	if strings.TrimSpace(user.Email) != "" {
		image := a.gravatar(r.Context(), gravatarHash(user.Email))
		if len(image.data) > 0 {
			w.Header().Set("Content-Type", image.mime)
			w.Write(image.data)
			return
		}
	}
	// A missing Gravatar or an unavailable upstream never leaves a broken image.
	initial := html.EscapeString(model.Initial(user.Name))
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="#717171"/><text x="32" y="34" text-anchor="middle" dominant-baseline="middle" font-family="system-ui,sans-serif" font-size="27" fill="white">%s</text></svg>`, initial)
}

func (a *App) gravatar(ctx context.Context, hash string) avatarImage {
	now := time.Now()
	a.avatars.mu.Lock()
	if cached, ok := a.avatars.images[hash]; ok && now.Before(cached.expires) {
		a.avatars.mu.Unlock()
		return cached
	}
	a.avatars.mu.Unlock()
	value, _, _ := a.avatars.group.Do(hash, func() (any, error) {
		image := a.fetchGravatar(ctx, hash)
		a.avatars.mu.Lock()
		if a.avatars.images == nil {
			a.avatars.images = make(map[string]avatarImage)
		}
		if len(a.avatars.images) >= 256 {
			for key, item := range a.avatars.images {
				if time.Now().After(item.expires) {
					delete(a.avatars.images, key)
				}
			}
			if len(a.avatars.images) >= 256 {
				for key := range a.avatars.images {
					delete(a.avatars.images, key)
					break
				}
			}
		}
		a.avatars.images[hash] = image
		a.avatars.mu.Unlock()
		return image, nil
	})
	return value.(avatarImage)
}

func (a *App) fetchGravatar(ctx context.Context, hash string) avatarImage {
	fallback := avatarImage{expires: time.Now().Add(10 * time.Minute)}
	client := a.AvatarHTTP
	if client == nil {
		client = defaultAvatarHTTP
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gravatar.com/avatar/"+hash+"?s=64&r=g&d=404", nil)
	if err != nil {
		return fallback
	}
	resp, err := client.Do(req)
	if err != nil {
		fallback.expires = time.Now().Add(2 * time.Minute)
		return fallback
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10+1))
	if err != nil || len(data) == 0 || len(data) > 512<<10 {
		return fallback
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return avatarImage{data: data, mime: mime, expires: time.Now().Add(time.Hour)}
	default:
		return fallback
	}
}
