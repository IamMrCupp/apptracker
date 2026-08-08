// Package auth provides optional single-password gating with signed cookie sessions.
//
// If no password is configured the middleware is a no-op, so the app runs open
// (intended for use behind a private cluster boundary). When a password is set,
// clients must POST it to /api/login to receive an HMAC-signed session cookie.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const cookieName = "apptracker_session"
const sessionTTL = 30 * 24 * time.Hour

// Authenticator holds the configured password and signing key.
type Authenticator struct {
	password string
	key      []byte
	now      func() time.Time
}

// New builds an Authenticator. An empty password disables auth entirely.
// If signingKey is empty a random one is generated (sessions reset on restart).
func New(password, signingKey string) (*Authenticator, error) {
	key := []byte(signingKey)
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate signing key: %w", err)
		}
	}
	return &Authenticator{password: password, key: key, now: time.Now}, nil
}

// Enabled reports whether a password gate is active.
func (a *Authenticator) Enabled() bool { return a.password != "" }

// sign returns "exp.sig" where sig = HMAC(key, exp).
func (a *Authenticator) sign(exp int64) string {
	msg := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(msg))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return msg + "." + sig
}

// valid reports whether a cookie token is authentic and unexpired.
func (a *Authenticator) valid(token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	expected := a.sign(exp)
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return false
	}
	return a.now().Unix() < exp
}

// checkPassword compares in constant time.
func (a *Authenticator) checkPassword(pw string) bool {
	return subtle.ConstantTimeCompare([]byte(pw), []byte(a.password)) == 1
}

// isTLS reports whether the request reached the app over HTTPS.
//
// X-Forwarded-Proto is only meaningful behind a proxy that sets it, which is
// the intended deployment (an ingress terminating TLS). A client talking to the
// app directly could spoof the header — but doing so only opts them into a
// stricter cookie, so the blast radius is nil.
func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// LoginHandler accepts {"password": "..."} and sets a session cookie on success.
func (a *Authenticator) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !a.checkPassword(body.Password) {
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	exp := a.now().Add(sessionTTL).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    a.sign(exp),
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(exp, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

// LogoutHandler clears the session cookie.
func (a *Authenticator) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// Attributes must match the cookie being cleared, or the browser keeps it.
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: isTLS(r), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// Authed reports whether the request carries a valid session (always true when disabled).
func (a *Authenticator) Authed(r *http.Request) bool {
	if !a.Enabled() {
		return true
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return a.valid(c.Value)
}

// Middleware guards API routes. Unauthenticated requests get 401.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Authed(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}
