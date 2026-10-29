package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/web"
)

const portalMaxBody = 32 << 20

type PortalAPIConfig struct {
	Key                []byte
	Issuer             string
	Hosts              map[string]bool
	AllowDevIdentities bool
}
type portalActor struct {
	Subject  string `json:"subject"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	CanStaff bool   `json:"canStaff"`
}
type portalIdentity struct {
	User                               model.User
	Staff, Manage, CanStaff, CanManage bool
	Actor                              portalActor
}
type portalIdentityKey struct{}

var portalHex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var portalHex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var portalHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

func PortalAPIFromEnvironment() (*PortalAPIConfig, error) {
	secret, hosts := os.Getenv("PORTAL_API_SIGNING_KEY"), os.Getenv("PORTAL_API_HOSTNAMES")
	if secret == "" && hosts == "" {
		return nil, nil
	}
	if !portalHex64.MatchString(secret) || hosts == "" {
		return nil, errors.New("Portal API needs a 32-byte hexadecimal signing key and exact hostnames")
	}
	key, _ := hex.DecodeString(secret)
	config := &PortalAPIConfig{Key: key, Issuer: "vproxy-portal", Hosts: map[string]bool{}, AllowDevIdentities: os.Getenv("PORTAL_API_ALLOW_DEV_IDENTITIES") == "true"}
	for _, host := range strings.Split(hosts, ",") {
		host = strings.TrimSpace(host)
		if !portalHost.MatchString(host) || strings.Contains(host, "..") {
			return nil, errors.New("invalid Portal API hostname")
		}
		config.Hosts[host] = true
	}
	return config, nil
}

// validPortalSubject accepts an empty subject, since accounts are matched by
// email; a supplied one is still bounded and may not claim a system identity.
func validPortalSubject(subject string, allowDev bool) bool {
	if subject == "" {
		return true
	}
	return len(subject) >= 3 && len(subject) <= 255 && utf8.ValidString(subject) && strings.IndexFunc(subject, unicode.IsControl) < 0 &&
		!strings.HasPrefix(subject, "system:") && (allowDev || !strings.HasPrefix(subject, "portal-dev|"))
}

func portalJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
func portalError(w http.ResponseWriter, status int, code string) {
	portalJSON(w, status, map[string]any{"error": code})
}
func (a *App) portalFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, database.ErrNotFound):
		portalError(w, 404, "not_found")
	case errors.Is(err, database.ErrInvalid), errors.Is(err, database.ErrAnnouncementLimit):
		portalError(w, 422, "invalid")
	case errors.Is(err, database.ErrConflict):
		portalError(w, 409, "conflict")
	case errors.Is(err, database.ErrClosed):
		portalError(w, 409, "closed")
	case errors.Is(err, database.ErrCommitUnknown):
		portalError(w, 503, "outcome_unknown")
	default:
		a.Log.Error("portal API operation failed", "error_type", fmt.Sprintf("%T", err))
		portalError(w, 503, "unavailable")
	}
}
func portalCurrent(r *http.Request) portalIdentity {
	return r.Context().Value(portalIdentityKey{}).(portalIdentity)
}
func (a *App) portalProtected(staff bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config := a.PortalAPI
		// No browser credentials, ambient sessions or CORS-based authentication.
		if config == nil || r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Content-Encoding") != "" {
			portalError(w, 401, "unauthorized")
			return
		}
		values := make([]string, 6)
		for i, name := range []string{"X-Portal-Issuer", "X-Portal-Timestamp", "X-Portal-Nonce", "X-Portal-Actor", "X-Portal-Body-SHA256", "X-Portal-Signature"} {
			if len(r.Header.Values(name)) != 1 {
				portalError(w, 401, "unauthorized")
				return
			}
			values[i] = r.Header.Get(name)
		}
		issuer, stamp, nonce, actorRaw, digest, signature := values[0], values[1], values[2], values[3], values[4], values[5]
		timestamp, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil || issuer != config.Issuer || !portalHex32.MatchString(nonce) || !portalHex64.MatchString(digest) || !portalHex64.MatchString(signature) || len(actorRaw) > 4096 || timestamp < time.Now().Unix()-60 || timestamp > time.Now().Unix()+60 {
			portalError(w, 401, "unauthorized")
			return
		}
		if len(r.Header.Values("Content-Type")) > 1 {
			portalError(w, 401, "unauthorized")
			return
		}
		message := strings.Join([]string{"vproxy-support-v1", issuer, r.Method, r.URL.RequestURI(), stamp, nonce, actorRaw, r.Header.Get("Content-Type"), digest}, "\n")
		mac := hmac.New(sha256.New, config.Key)
		mac.Write([]byte(message))
		supplied, _ := hex.DecodeString(signature)
		if !hmac.Equal(mac.Sum(nil), supplied) {
			portalError(w, 401, "unauthorized")
			return
		}
		var actor portalActor
		raw, err := base64.RawURLEncoding.DecodeString(actorRaw)
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err != nil || !utf8.Valid(raw) || decoder.Decode(&actor) != nil || decoder.Decode(&struct{}{}) != io.EOF || !config.Hosts[actor.Hostname] || !validPortalSubject(actor.Subject, config.AllowDevIdentities) || len([]rune(actor.Name)) > 80 || strings.TrimSpace(actor.Name) == "" || !utf8.ValidString(actor.Name) || strings.IndexFunc(actor.Name, unicode.IsControl) >= 0 {
			portalError(w, 401, "unauthorized")
			return
		}
		// Accounts are matched by email; the normalized (lower-case) address is the key.
		email, err := database.NormalizeInviteEmails([]string{actor.Email}, "")
		if err != nil || len(email) != 1 {
			portalError(w, 401, "unauthorized")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, portalMaxBody))
		if err != nil {
			portalError(w, 413, "too_large")
			return
		}
		actual := sha256.Sum256(body)
		if hex.EncodeToString(actual[:]) != digest {
			portalError(w, 401, "unauthorized")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		claimed, err := a.Store.ClaimPortalNonce(ctx, issuer, nonce, time.Unix(timestamp, 0).Add(2*time.Minute))
		if err != nil {
			a.portalFailure(w, err)
			return
		}
		if !claimed {
			portalError(w, 409, "replayed_request")
			return
		}
		user, err := a.Store.PortalUser(ctx, actor.Name, email[0])
		if err != nil {
			a.portalFailure(w, err)
			return
		}
		allowed := actor.CanStaff && user.Staff()
		if staff && !allowed {
			portalError(w, 403, "forbidden")
			return
		}
		identity := portalIdentity{User: user, Staff: staff && allowed, Manage: staff && allowed && user.Role == "admin", CanStaff: allowed, CanManage: allowed && user.Role == "admin", Actor: actor}
		// Native Attachment/Invites use User.Staff(). Member API calls must never inherit staff power.
		if !identity.Staff {
			identity.User.Role = "user"
		}
		r = r.WithContext(context.WithValue(ctx, portalIdentityKey{}, identity))
		r.Body = io.NopCloser(bytes.NewReader(body))
		r = r.WithContext(database.WithRequestInfo(r.Context(), database.RequestInfo{UserAgent: "vProxy Portal", Brand: web.BrandKey(r.Context())}))
		next(w, r)
	}
}
