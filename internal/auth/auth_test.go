package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDisabledWhenNoPassword(t *testing.T) {
	a, err := New("", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Enabled() {
		t.Fatal("expected auth disabled")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/entries", nil)
	if !a.Authed(req) {
		t.Fatal("expected all requests authed when disabled")
	}
}

func TestLoginSetsCookieAndAuthorizes(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}

	// wrong password -> 401
	rr := httptest.NewRecorder()
	a.LoginHandler(rr, httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"password":"nope"}`)))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad password, got %d", rr.Code)
	}

	// correct password -> cookie
	rr = httptest.NewRecorder()
	a.LoginHandler(rr, httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"password":"hunter2"}`)))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie")
	}

	// cookie authorizes a follow-up request
	req := httptest.NewRequest(http.MethodGet, "/api/entries", nil)
	req.AddCookie(cookies[0])
	if !a.Authed(req) {
		t.Fatal("expected valid cookie to authorize")
	}
}

func TestMiddlewareBlocksUnauthed(t *testing.T) {
	a, _ := New("secret", "k")
	called := false
	h := a.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/entries", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if called {
		t.Fatal("next handler should not have been called")
	}
}

func TestTamperedAndExpiredCookieRejected(t *testing.T) {
	a, _ := New("secret", "k")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "9999999999.forgedsig"})
	if a.Authed(req) {
		t.Fatal("forged signature should be rejected")
	}

	// expired but correctly signed
	a.now = func() time.Time { return time.Unix(1000, 0) }
	token := a.sign(500) // exp in the past relative to now()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	if a.Authed(req2) {
		t.Fatal("expired cookie should be rejected")
	}
}
