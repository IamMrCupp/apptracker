package auth

import (
	"crypto/tls"
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

// loginCookie drives a login and returns the Set-Cookie the handler produced.
func loginCookie(t *testing.T, a *Authenticator, decorate func(*http.Request)) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"password":"hunter2"}`))
	if decorate != nil {
		decorate(req)
	}
	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("login failed: %d", rr.Code)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

// Plain HTTP has to keep working — `docker compose up` on http://localhost is
// the documented quickstart, and a Secure cookie would silently break it.
func TestCookieNotSecureOverPlainHTTP(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if c := loginCookie(t, a, nil); c.Secure {
		t.Fatal("expected Secure unset on a plain HTTP request")
	}
}

func TestCookieSecureBehindTLSTerminatingProxy(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	c := loginCookie(t, a, func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if !c.Secure {
		t.Fatal("expected Secure when X-Forwarded-Proto is https")
	}
}

func TestCookieSecureOnDirectTLS(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	c := loginCookie(t, a, func(r *http.Request) {
		r.TLS = &tls.ConnectionState{}
	})
	if !c.Secure {
		t.Fatal("expected Secure when the request arrived over TLS")
	}
}

// attempt drives one login with the given password and returns the status code.
func attempt(t *testing.T, a *Authenticator, pw string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"password":"`+pw+`"}`))
	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)
	return rr.Code
}

func TestLoginLocksOutAfterRepeatedFailures(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLoginFailures; i++ {
		if code := attempt(t, a, "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401", i+1, code)
		}
	}
	// One past the threshold: locked out, and the *correct* password must not
	// get through either — otherwise the lockout is trivially bypassable.
	if code := attempt(t, a, "wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429 after %d failures", code, maxLoginFailures)
	}
	if code := attempt(t, a, "hunter2"); code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429 — lockout must apply to the correct password too", code)
	}
}

func TestLockoutSetsRetryAfter(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLoginFailures; i++ {
		attempt(t, a, "wrong")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"password":"wrong"}`))
	rr := httptest.NewRecorder()
	a.LoginHandler(rr, req)
	if got := rr.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected a Retry-After header on 429")
	}
}

func TestLockoutExpires(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	a.now = func() time.Time { return base }
	for i := 0; i < maxLoginFailures; i++ {
		attempt(t, a, "wrong")
	}
	if code := attempt(t, a, "hunter2"); code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429 while locked", code)
	}
	a.now = func() time.Time { return base.Add(lockoutWindow + time.Second) }
	if code := attempt(t, a, "hunter2"); code != http.StatusNoContent {
		t.Fatalf("got %d, want 204 once the lockout expired", code)
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	a, err := New("hunter2", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLoginFailures-1; i++ {
		attempt(t, a, "wrong")
	}
	if code := attempt(t, a, "hunter2"); code != http.StatusNoContent {
		t.Fatalf("got %d, want 204 just under the threshold", code)
	}
	// Counter reset, so the budget is full again.
	for i := 0; i < maxLoginFailures-1; i++ {
		if code := attempt(t, a, "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d after reset: got %d, want 401", i+1, code)
		}
	}
}
