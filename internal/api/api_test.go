package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/IamMrCupp/apptracker/internal/auth"
	"github.com/IamMrCupp/apptracker/internal/store"
)

func newTestServer(t *testing.T, password string) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a, err := auth.New(password, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Auth: a}
	return s, s.Routes()
}

func createEntry(t *testing.T, h http.Handler, e store.Entry) store.Entry {
	t.Helper()
	body, _ := json.Marshal(e)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/entries", bytes.NewReader(body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rr.Code, rr.Body)
	}
	var out store.Entry
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCRUDFlow(t *testing.T) {
	_, h := newTestServer(t, "")

	created := createEntry(t, h, store.Entry{Kind: store.KindApplication, Entity: "Acme", Status: "Applied"})
	if created.ID == 0 {
		t.Fatal("expected id")
	}

	// list
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/entries?kind=application", nil))
	var list []store.Entry
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}

	// update
	created.Status = "Offer"
	body, _ := json.Marshal(created)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/entries/"+strconv.FormatInt(created.ID, 10), bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", rr.Code)
	}

	// delete
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/entries/"+strconv.FormatInt(created.ID, 10), nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rr.Code)
	}
}

func TestCreateRejectsBadKind(t *testing.T) {
	_, h := newTestServer(t, "")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/entries",
		strings.NewReader(`{"kind":"bogus","entity":"x"}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUpdateMissingReturns404(t *testing.T) {
	_, h := newTestServer(t, "")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/entries/999",
		strings.NewReader(`{"kind":"application"}`)))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestJSONExportImportRoundTrip(t *testing.T) {
	s, h := newTestServer(t, "")
	createEntry(t, h, store.Entry{Kind: store.KindApplication, Entity: "One", Status: "Applied"})
	createEntry(t, h, store.Entry{Kind: store.KindNetworking, Entity: "Two"})

	// export json
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/export?format=json", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("export: %d", rr.Code)
	}
	exported := rr.Body.Bytes()

	// wipe then re-import
	if err := s.Store.Clear(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/import?format=json", bytes.NewReader(exported)))
	if rr.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d (%s)", rr.Code, rr.Body)
	}
	all, _ := s.Store.List(context.Background(), "")
	if len(all) != 2 {
		t.Fatalf("expected 2 after round-trip, got %d", len(all))
	}
}

func TestCSVExportImportRoundTrip(t *testing.T) {
	s, h := newTestServer(t, "")
	createEntry(t, h, store.Entry{Kind: store.KindApplication, Entity: "Comma, Inc", Notes: "line1\nline2", Status: "Applied"})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/export?format=csv", nil))
	csvData := rr.Body.Bytes()
	if !bytes.Contains(csvData, []byte("kind")) {
		t.Fatal("expected csv header")
	}

	s.Store.Clear(context.Background(), "")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/import?format=csv", bytes.NewReader(csvData)))
	if rr.Code != http.StatusOK {
		t.Fatalf("csv import: expected 200, got %d (%s)", rr.Code, rr.Body)
	}
	all, _ := s.Store.List(context.Background(), "")
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
	if all[0].Entity != "Comma, Inc" || all[0].Notes != "line1\nline2" {
		t.Fatalf("csv did not preserve special chars: %+v", all[0])
	}
}

func TestImportRejectsBadKind(t *testing.T) {
	_, h := newTestServer(t, "")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/import?format=json",
		strings.NewReader(`[{"kind":"nope"}]`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAuthProtectsAPI(t *testing.T) {
	_, h := newTestServer(t, "s3cret")

	// unauthenticated list -> 401
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/entries", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	// login
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"s3cret"}`)))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("login: expected 204, got %d", rr.Code)
	}
	cookies := rr.Result().Cookies()

	// authenticated list -> 200
	req := httptest.NewRequest(http.MethodGet, "/api/entries", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with cookie, got %d", rr.Code)
	}
}

func TestHealthzOpen(t *testing.T) {
	_, h := newTestServer(t, "s3cret") // even with auth on, health is public
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "ok" {
		t.Fatalf("healthz: %d %q", rr.Code, rr.Body.String())
	}
}
